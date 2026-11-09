# Free API 网关

把多个第三方 AI 站点的免费能力，统一暴露成三套兼容 API。每个站点是一个独立插件进程，用任意语言实现。

本仓库**只包含网关核心**（协议转换、路由、限流、凭证存储、控制台），站点插件不随仓库分发。插件接口规范见 [docs/plugin-requirements.md](docs/plugin-requirements.md)，按规范实现并在配置里声明即可接入。下文提到的 15 个站点是参考部署的实测记录，插件实现请自行构建。

## 支持的协议

| 端点 | 兼容对象 |
|---|---|
| `POST /v1/chat/completions` | OpenAI Chat Completions（含 SSE 流式） |
| `POST /v1/responses` | OpenAI Responses（含 `response.output_text.delta` 等事件） |
| `POST /v1/messages` | Anthropic Messages（含 `message_start` / `content_block_delta` / `message_stop`） |
| `GET /v1/models` | 模型目录，带工具能力与认证状态 |
| `GET /healthz` | 健康检查 |

三套协议共用同一套插件，客户端用哪套 SDK 都能直接指过来。

## 设计约束

网关**不内置**临时邮箱、自动注册、验证码识别等能力。需要登录的站点只在配置里声明需要哪些凭证字段：

```yaml
auth:
  type: cookie            # cookie / bearer / api_key / oauth / none
  required_fields: [cookie]
  optional_fields: [csrf_token]
```

凭证由使用者在控制台录入，用 AES-256-GCM 加密落盘，列表接口只回字段名不回明文。**未配置凭证的插件不参与路由**，`/v1/models` 里对应条目 `auth.configured` 为 `false`。

## 工具档位

每个插件声明一档，决定它能否承接带工具的请求：

| 档位 | 含义 | 路由行为 |
|---|---|---|
| `native` | 站点内部 API 原生支持 `tools` 参数 | 承接带工具请求，`tools` schema 原样透传 |
| `builtin` | 站点自带联网/代码执行，但不接受自定义 schema | 仅承接无工具请求；`builtin_tools` 以平台能力形式暴露 |
| `none` | 纯对话 | 仅承接无工具请求 |

请求带函数工具时，网关只在 `native` 档位里选来源；没有可用来源会明确报错，不会静默降级。

## 快速开始

```bash
go build -o bin/gateway ./cmd/gateway

export FREE_API_CRED_KEY="换成一个长随机串"   # 凭证加密密钥，必须自己保管
./bin/gateway -config configs/gateway.yaml
```

打开 `http://localhost:18080/` 是控制台，可以管理站点凭证、代理池、网关 API Key 与登录密码。

插件按 [docs/plugin-requirements.md](docs/plugin-requirements.md) 实现后放到 `plugins/<name>/`，在 `configs/gateway.yaml` 声明即可接入。

## 插件开发

插件是普通可执行文件，网关通过 stdin/stdout 每行一个 JSON 通信。

网关发来：

```json
{"jsonrpc":"2.0","id":"p-1","method":"chat","params":{
  "model":"xxx","system":"","messages":[{"role":"user","content":"hi"}],
  "tools":[{"type":"function","name":"search","parameters":{}}],
  "max_tokens":1024,"temperature":0.7,
  "credential":{"cookie":"..."}
}}
```

插件回推事件：

```json
{"jsonrpc":"2.0","id":"p-1","method":"event","params":{"type":"delta","text":"你"}}
{"jsonrpc":"2.0","id":"p-1","method":"event","params":{"type":"tool_call","tool_calls":[{"id":"c1","name":"search","arguments":"{\"q\":\"x\"}"}]}}
{"jsonrpc":"2.0","id":"p-1","method":"event","params":{"type":"usage","usage":{"prompt_tokens":12,"completion_tokens":5}}}
{"jsonrpc":"2.0","id":"p-1","method":"event","params":{"type":"done","finish":"stop"}}
```

必须实现 `models` 方法（返回模型清单），网关启动时会先探测它。`done` 或 `error` 是流结束标记，插件推完即结束，不必再返回最终应答。

`plugins/echo` 曾是本仓库的参考实现（覆盖协议与工具闭环），现已随插件一起移出本仓库；协议细节与验收清单统一见 [docs/plugin-requirements.md](docs/plugin-requirements.md)。

## 目录结构

```
cmd/gateway/          入口：加载配置、拉起插件、启动 HTTP
internal/config/      YAML 配置
internal/credential/  凭证加密存储
internal/protocol/    三套协议的解析与渲染
internal/plugin/      插件 stdio 宿主与注册表
internal/gateway/     路由、限流、HTTP 端点、控制台
plugins/              站点插件（不在本仓库分发）
configs/              网关配置
docs/                 插件规范
```

## 参考部署已实测的站点（15 个，插件不随仓库分发）

下表是参考部署的实测记录，用于说明接入形态；对应插件实现需按 [docs/plugin-requirements.md](docs/plugin-requirements.md) 自行构建。通用插件 `openai-compat` 的思路是用同一个二进制接入所有 OpenAI 兼容端点——新增站点只需改配置，不写代码。

| 插件 | 站点 | 认证 | 工具档位 | 说明 |
|---|---|---|---|---|
| `pollinations` | text.pollinations.ai | 无需 | `native` | 匿名可用，**实测工具调用闭环通过** |
| `aihorde` | aihorde.net | 无需（匿名 key） | `none` | 26 个在线文本模型，异步队列 |
| `echo` | 无（参考实现） | 无需 | `native` | 验证协议与工具闭环 |
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

**工具档位分布**：`native` 14 个，`none` 1 个。

## 通用插件的用法

新增一个兼容站点只需在 `configs/gateway.yaml` 加一段（`openai-compat` 按 [docs/plugin-requirements.md](docs/plugin-requirements.md) 自行构建，用环境变量描述端点与模型）：

```yaml
- name: 站点名
  command: ./plugins/openai-compat/bin/openai-compat
  priority: 20
  tools: native
  env:
    COMPAT_BASE_URL: "https://api.example.com/v1"
    COMPAT_MODELS: "model-a,model-b"
  models: [model-a, model-b]
  auth:
    type: api_key
    required_fields: [api_key]
```

可选环境变量：`COMPAT_TOOLS=false` 声明不支持工具；`COMPAT_EXTRA_HEADERS` 传额外请求头（部分聚合站要求 Referer / X-Title）。

## 凭证流程（已实测）

```
录入前 → /v1/models 标记 configured:false → 不参与路由
       → 带工具请求返回「没有可用来源」
录入后 → 立即变为 ready:true → 参与路由
```

凭证只写不读，接口永不回明文。

## 站点探针结论

探针脚本在 `scripts/probe.mjs`（静态采集）与 `scripts/probe-chat.mjs`（真实发消息）。对三个主流站点的实测结果：

| 站点 | 状态 | 阻塞点 |
|---|---|---|
| lmarena.ai | 内部 API 被 Cloudflare 拦截 | `/nextjs-api/model-catalog` 返回 403，需 `cf_clearance` |
| duck.ai | 有干净的 REST 端点 | 对话需先算混淆 JS 生成的 `x-vqd-hash-1` 挑战值 |
| huggingface.co/chat | 有 MCP 端点 | 输入框初始为 `inert`，需先完成模型选择初始化 |

这三个站点的结论说明：纯 HTTP 复刻的关键不在「端点在哪」，而在**会话凭证与反爬挑战的维持成本**。选站点时应优先收匿名可用、无挑战的，而不是硬啃 Cloudflare。

## 尚未接入的部分

站点插件还在扩充。每个新站点插件需要先跑探针拿到：内部端点、必需 header、流式帧格式、工具字段的真实序列化形态，然后写成纯 HTTP 插件。
