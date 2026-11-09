# Free API 网关 — 项目计划

> 目标：把多个第三方 AI 站点的免费能力，统一暴露成兼容 OpenAI / Anthropic 的三套 API。
> 当前状态：网关主干已完成并验证；站点插件在扩充中。

---

## 一、已完成部分

### 1.1 协议层（已完成）

三套 API 共用一套统一中间结构，避免两两互转产生 6 份转换代码。

| 端点 | 兼容对象 | 状态 |
|---|---|---|
| `POST /v1/chat/completions` | OpenAI Chat Completions | 完成，含 SSE 流式、tool_calls、`[DONE]` |
| `POST /v1/responses` | OpenAI Responses | 完成，含 `response.created` / `output_text.delta` / `function_call_arguments.delta` / `response.completed` |
| `POST /v1/messages` | Anthropic Messages | 完成，含 `message_start` / `content_block_delta` / `message_stop` |
| `GET /v1/models` | 模型目录 | 完成，带 `capabilities.tools` 与 `auth.configured` |
| `GET /healthz` | 健康检查 | 完成 |

统一中间表示：`Request{Model, System, Messages, Tools, ToolChoice, MaxTokens, Temperature, Stream}`，事件流 `delta / tool_call / usage / done / error`。

### 1.2 插件宿主（已完成）

插件是独立可执行文件，通过 **stdio JSON-RPC** 与网关通信，每行一个 JSON。

- 不占端口，无端口冲突
- 可用任意语言实现（Go / Node / Python）
- 网关负责进程生命周期、健康探测、崩溃重启
- `done` / `error` 即视为流结束，不阻塞等待

### 1.3 凭证（已完成）

- AES-256-GCM 加密落盘，密钥来自 `FREE_API_CRED_KEY` 环境变量，不落盘
- **只写不读**：列表接口只回字段名与 `configured` 布尔值
- 按插件声明注入，插件拿不到未声明的字段
- 未配置凭证的插件不参与路由，`/v1/models` 标记 `configured: false`

### 1.4 路由与限流（已完成）

- **工具档位路由**：请求带函数工具时只在 `native` 档位选来源，`builtin` / `none` 排除
- **插件级 RPS 覆盖**：对齐各站点真实免费配额（如 pollinations「每 45 秒 1 次」）
- 并发信号量、令牌桶、失败自动切下一候选
- 无可用来源时明确报错，不静默降级

### 1.5 控制台（已完成）

Notion 风格单页，嵌在二进制里：

- 米色底 `#f7f6f3`、正文 `#37352f`、细边框
- 克制动效，无大圆角 / 渐变 / 位移动画
- 四个面板：站点插件、模型目录、凭证管理、接口说明
- 站点卡片标注工具档位与凭证状态

### 1.6 探针工具（已完成）

- `scripts/probe.mjs`：静态采集，拦截 fetch/XHR/WebSocket
- `scripts/probe-chat.mjs`：真实发消息，抓聊天端点与 SSE 帧

### 1.7 已有插件（15 个）

| 插件 | 站点 | 认证 | 工具档位 | 状态 |
|---|---|---|---|---|
| `echo` | 无（参考实现） | 无需 | `native` | 验证协议与工具闭环 |
| `pollinations` | text.pollinations.ai | 无需 | `native` | **实测工具调用闭环通过** |
| `aihorde` | aihorde.net | 无需（匿名 key） | `none` | 26 个在线文本模型，异步队列 |
| `openrouter` | openrouter.ai | `api_key` | `native` | 含免费模型 |
| `groq` | api.groq.com | `api_key` | `native` | 免费额度大、速度快 |
| `cerebras` | api.cerebras.ai | `api_key` | `native` | 推理极快 |
| `mistral` | api.mistral.ai | `api_key` | `native` | Mistral 系列 |
| `together` | api.together.xyz | `api_key` | `native` | 开源权重模型池大 |
| `deepinfra` | api.deepinfra.com | `api_key` | `native` | 按量计费 |
| `siliconflow` | api.siliconflow.cn | `api_key` | `native` | 国内直连 |
| `sambanova` | api.sambanova.ai | `api_key` | `native` | 免费层吞吐高 |
| `novita` | api.novita.ai | `api_key` | `native` | 部分模型免费 |
| `nebius` | api.studio.nebius.ai | `api_key` | `native` | 免费层含开源模型 |
| `nvidia` | integrate.api.nvidia.com | `api_key` | `native` | 84 个模型可查 |
| `cohere` | api.cohere.ai | `api_key` | `native` | Command 系列 |

**工具档位分布**：`native` 14 个，`none` 1 个。达成 15 个站点目标，A 类占 14/15。

### 1.7.1 通用兼容插件（关键设计）

`plugins/openai-compat` 用**同一个二进制**接入所有 OpenAI 兼容端点：

```yaml
- name: 任意站点名
  command: ./plugins/openai-compat/bin/openai-compat
  env:
    COMPAT_BASE_URL: "https://api.example.com/v1"
    COMPAT_MODELS: "model-a,model-b"
    COMPAT_TOOLS: "true"          # 可选
    COMPAT_EXTRA_HEADERS: "HTTP-Referer:https://x|X-Title:y"  # 可选
  auth:
    type: api_key
    required_fields: [api_key]
```

新增站点从「写插件 + 编译」降为「改配置」，这是站点池能快速扩到 15 个的原因。

### 1.8 验证

`scripts/verify.sh` 覆盖 23 项断言，全部通过：

- 三套协议的非流式与流式
- 工具调用两轮闭环（第一轮 `tool_calls` → 回填 → 第二轮读到结果）
- 错误处理（未知模型、Anthropic 缺 `max_tokens`）
- 凭证管理（写入生效、明文不泄漏）

---

## 二、关键实测结论

### 2.1 三个主流站点的阻塞点

| 站点 | 端点状态 | 真正阻塞点 | 结论 |
|---|---|---|---|
| lmarena.ai | `/nextjs-api/model-catalog` 端点清楚 | Cloudflare 直接 403，需维持 `cf_clearance` | 成本高 |
| duck.ai | `/duckchat/v1/*` 全部 200，端点干净 | 对话需先算混淆 JS 生成的 `x-vqd-hash-1` | 成本高 |
| huggingface.co/chat | 有 MCP 工具端点 | 输入框初始 `inert`，需完成模型选择初始化 | 成本中 |

**共性**：找端点不难，难的是维持会话凭证与反爬挑战。选站应优先匿名、无挑战的源。

### 2.2 免凭证端点实测

| 端点 | 结果 |
|---|---|
| `text.pollinations.ai/openai` | **200，匿名可用，原生 tools** |
| `aihorde.net/api/v2/*` | **202，匿名 key `0000000000` 可用** |
| `api.groq.com` | 401，需 key |
| `api.cerebras.ai` | 401，需 key |
| `api.together.xyz` | 401，需 key |
| `api.deepinfra.com` | 401，需 key |

---

## 三、待办事项

### 3.1 站点扩充（进行中）

**目标**：凑够 15 个站点，A 类（原生 tools）优先。

**筛选标准**（优先级从高到低）：

1. 免凭证 + 无挑战 + 原生支持 tools → 直接接入
2. 免凭证 + 无挑战 + 无 tools → 接入，标注「无法使用工具」
3. 需 key 但注册简单（邮箱即可）+ 原生 tools → 接入，凭证由使用者录入
4. 需复杂反爬/手机号注册 → 暂缓，或明确标注成本

**待探候选**：

- 免凭证类：Cloudflare Workers AI Playground、GitHub Models、NVIDIA Build、Segmind
- 聚合类：OpenRouter（key 已有适配器）、Together、Fireworks、DeepInfra、SambaNova、Nebius、Chutes
- 聊天类：Poe、You.com、Phind、Genspark
- 编码类：Cursor、Cline、Continue、OpenHands

**每个站点接入流程**：

```
跑探针 → 确认端点与帧格式 → 判定工具档位 → 写纯 HTTP 插件
→ 配 configs/gateway.yaml（含 per_plugin_rps）→ 跑 verify.sh 回归
```

### 3.2 工具能力增强

- **B 类站点支持**：平台托管工具（联网搜索、代码执行）以 `builtin_tools` 形式暴露
- **工具调用循环**：客户端回填工具结果后，网关保持会话上下文重发（当前依赖插件自身维护）
- **流式工具调用**：工具参数分片到达时的增量拼接（当前按整块处理）

### 3.3 可观测性

- 请求审计日志：每次请求走了哪个插件、耗时、失败原因
- 控制台实时请求流
- 插件健康度趋势（成功率、平均延迟）
- 配额消耗看板（各站点剩余额度估算）

### 3.4 稳定性

- 插件崩溃自动重启（已有基础，需补退避策略）
- 熔断：连续失败 N 次后临时摘除该插件
- 重试：区分可重试（超时、5xx）与不可重试（4xx、配额耗尽）

### 3.5 部署

- Docker 镜像（多阶段构建，二进制 + 插件）
- 单文件交叉编译
- 环境变量与配置文件模板
- systemd / compose 示例

---

## 四、设计约束（不可变更）

1. **网关不内置临时邮箱、验证码识别、自动注册**
2. **需要登录的站点只声明字段需求**，凭证由使用者录入
3. **运行期不使用无头浏览器**，无头浏览器仅存在于探针期
4. **不收录模型厂商自家网页**（chat.deepseek.com、chat.gemini.google.com 等）
5. **工具档位是一级路由条件**，不做静默降级
6. **控制台严格遵循 Notion 风格**：无 `rounded-2xl`+、无渐变、无重阴影、无 translate/scale 动画

---

## 五、当前可运行状态

```bash
# 构建
go build -o bin/gateway ./cmd/gateway
go build -o plugins/echo/bin/echo ./plugins/echo
go build -o plugins/pollinations/bin/pollinations ./plugins/pollinations
go build -o plugins/aihorde/bin/aihorde ./plugins/aihorde
go build -o plugins/openrouter/bin/openrouter ./plugins/openrouter

# 启动
export FREE_API_CRED_KEY="换成长随机串"
./bin/gateway -config configs/gateway.yaml

# 控制台
open http://localhost:18080/

# 验证
./scripts/verify.sh
```

**已实测跑通的真实链路**：

```
Client → /v1/chat/completions → Gateway → pollinations → 模型返回
（工具调用：finish_reason: tool_calls，参数完整）
```

---

## 六、文件清单

```
cmd/gateway/main.go              入口：加载配置、拉起插件、启动 HTTP
internal/config/config.go        YAML 配置与校验
internal/credential/store.go     凭证加密存储
internal/protocol/               三套协议的解析与渲染
  canonical.go                   统一中间结构
  openai_chat.go                 OpenAI Chat
  openai_responses.go            OpenAI Responses
  anthropic.go                   Anthropic Messages
  sse.go / util.go / errors.go   公共设施
internal/plugin/                 插件 stdio 宿主与注册表
internal/gateway/                路由、限流、HTTP 端点、控制台
plugins/echo/                    参考实现
plugins/pollinations/            免凭证真实站点（A 类）
plugins/aihorde/                 AI Horde（C 类）
plugins/openrouter/              HTTP 适配器示例
scripts/probe.mjs                静态探针
scripts/probe-chat.mjs           交互探针
scripts/verify.sh                端到端验证
```

---

## 七、风险与限制

| 风险 | 影响 | 应对 |
|---|---|---|
| 站点免费政策随时收紧 | 插件失效 | 插件热插拔，配置可禁用；多来源冗余 |
| 反爬升级（Cloudflare / 挑战 JS） | 纯 HTTP 调用被拒 | 优先选无挑战站点；必要时人工更新凭证 |
| 站点配额极低（如 45 秒 1 次） | 吞吐受限 | `per_plugin_rps` 对齐真实配额，多站点轮换 |
| 异步站点排队不可控 | 延迟高 | 超时放宽；优先选同步站点的模型 |
| 上游返回格式变更 | 解析失败 | 插件内做防御性解析，失败降级到下一来源 |
