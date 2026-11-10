# Free API 网关

把多个第三方 AI 站点的能力，统一暴露成三套兼容 API。每个站点是一个独立插件进程，可用任意语言实现。

> **本仓库只包含网关本体**：协议转换、路由、限流、代理、凭证存储与控制台。
> 站点插件不随本仓库分发，实现规范见 [docs/plugin-requirements.md](docs/plugin-requirements.md)。

## 功能

- **三套协议**：同一套插件同时对外提供 OpenAI Chat Completions、OpenAI Responses、Anthropic Messages。
- **模型命名**：对外模型 ID 为 `插件/模型名`，例如 `aki/deepseek-v4.1-flash`；也兼容纯模型名。
- **统一代理**：网关给所有插件进程自动注入 `HTTP_PROXY`，按全局代理池轮换出口，插件无需自己实现代理。
- **控制台**：站点状态、模型目录、代理池、登录密码、网关 API Key 管理，全部内置。
- **凭证加密**：站点凭证用 AES-256-GCM 加密落盘，接口不回明文。
- **故障转移**：同一模型有多个来源时按优先级依次尝试；带工具请求只路由到支持工具透传的站点。
- **限流与并发**：插件级令牌桶、并发信号量、请求超时与重试。

## 支持的端点

| 端点 | 兼容对象 |
|---|---|
| `POST /v1/chat/completions` | OpenAI Chat Completions（含 SSE 流式） |
| `POST /v1/responses` | OpenAI Responses（含 `response.output_text.delta` 等事件） |
| `POST /v1/messages` | Anthropic Messages（含 `message_start` / `content_block_delta` / `message_stop`） |
| `GET /v1/models` | 模型目录，带工具能力与认证状态 |
| `GET /v1/models/{id...}` | 单个模型详情，支持 `插件/模型名` 这类带斜杠的 ID |
| `GET /healthz` | 健康检查 |

三套协议共用同一套插件，客户端用哪套 SDK 都能直接指过来。

## 快速开始

```bash
go build -o bin/gateway ./cmd/gateway

export FREE_API_CRED_KEY="换成一个长随机串"   # 凭证加密密钥，必须自己保管
./bin/gateway -config configs/gateway.yaml
```

打开 `http://localhost:18080/` 是控制台。

插件按 [docs/plugin-requirements.md](docs/plugin-requirements.md) 实现后放到 `plugins/<name>/`，在 `configs/gateway.yaml` 声明即可接入。插件代码、二进制、配置都不会进入本仓库。

## 模型命名

对外模型 ID 统一为 **`插件/模型名`**：

- `GET /v1/models` 返回的 `id` 已带插件前缀，例如 `aki/deepseek-v4.1-flash`；
- 请求时使用完整 ID 会锁定到该插件，斜杠后的纯模型名才会传给插件；
- 也兼容纯模型名：没有歧义时，会在所有提供该模型的插件间按优先级故障转移；
- 本身带斜杠的上游模型名（如 `meta-llama/...`）优先按完整模型名匹配，不会被误拆成插件前缀。

`configs/gateway.yaml` 里的 `models` 和插件 `models` 方法都写纯上游模型名，前缀由网关生成。

## 代理池

控制台的「代理池」是全局出口池：

- 网关启动插件进程时自动注入 `HTTP_PROXY` / `HTTPS_PROXY` / `ALL_PROXY`（含小写），指向内置的轮换代理入口；
- 插件只需使用标准 HTTP 客户端，**不需要自己实现代理**，也不需要在插件配置里写代理；
- 每次 HTTP 请求 / HTTPS CONNECT 从池中轮换出口，支持 `http`、`https`、`socks5`、`socks5h`，可带 `user:pass@`；
- 池为空时直连；某个出口失败会自动尝试下一个；
- 入口只监听 `127.0.0.1`，使用随机 Basic 认证，非插件进程无法直接滥用。

旧版插件若读取 `credential.proxy`，网关仍会按调用注入，保持兼容；新插件可以忽略该字段。

## 控制台

- **站点插件**：搜索站点或模型，查看运行状态、工具档位、账号数与调用统计。
- **模型目录**：按站点分组，支持搜索、收起/展开、分页。
- **代理池**：列表展示、弹窗添加、单条删除、分页，保存后所有站点立即生效。
- **设置**：
  - 控制台登录密码（只存加盐哈希，会话在内存中）；
  - 网关 API Key（表格展示状态、调用次数、最近调用时间，可停用/启用/删除）。
- **接口说明**：三套协议的调用示例。

## 配置

```yaml
server:
  addr: ":18080"
  # 网关自身的 API Key。留空表示 /v1 不鉴权（仅建议本机使用）。
  api_keys: []
  request_timeout: "180s"
  max_body_bytes: 8388608

limits:
  per_plugin_concurrency: 2   # 每个插件同时处理的请求数
  per_plugin_rps: 1           # 每个插件的每秒请求数
  max_retries: 2

plugins:
  - name: example
    command: ./plugins/example/bin/example
    priority: 20
    tools: native
    timeout: "120s"
    per_plugin_rps: 1
    env:
      EXAMPLE_BASE_URL: "https://api.example.com/v1"
    models:
      - example-model
    auth:
      type: api_key
      required_fields:
        - api_key
```

| 字段 | 说明 |
|---|---|
| `name` | 全局唯一，控制台与日志中显示的名称 |
| `command` | 可执行文件路径；相对路径按网关启动目录解析 |
| `args` / `dir` | 启动参数与工作目录，可选 |
| `env` | 传给插件的环境变量，可选 |
| `priority` | 数字越小越优先；同模型多来源时按此顺序故障转移 |
| `tools` | `native` / `builtin` / `none` |
| `timeout` / `per_plugin_rps` | 插件级超时与限流覆盖 |
| `models` | 该插件提供的纯模型名列表，必须包含 `models` 方法返回的 ID |
| `auth.type` | `none` / `api_key` / `bearer` / `cookie` / `oauth` |
| `auth.required_fields` | 缺失则插件不参与路由 |
| `auth.optional_fields` | 可选凭证字段 |

## 工具档位

| 档位 | 含义 | 路由行为 |
|---|---|---|
| `native` | 站点内部 API 原生支持 `tools` 参数 | 承接带工具请求，`tools` schema 原样透传 |
| `builtin` | 站点自带联网/代码执行，但不接受自定义 schema | 仅承接无工具请求；`builtin_tools` 以平台能力形式暴露 |
| `none` | 纯对话 | 仅承接无工具请求 |

请求带函数工具时，网关只在 `native` 档位里选来源；没有可用来源会明确报错，不会静默降级。

## 凭证与安全

- 站点凭证由使用者在控制台录入，用 AES-256-GCM 加密落盘；列表接口只回字段名，不回明文。
- 加密密钥来自环境变量 `FREE_API_CRED_KEY`，不落盘；换密钥后旧密文无法解密。
- 未配置必需凭证的插件不参与路由，`/v1/models` 中对应来源 `ready` 为 `false`。
- 控制台密码只存加盐哈希；登录会话只存在内存中，重启后需重新登录。
- 网关 API Key 支持多枚、停用/启用与按 Key 统计调用次数；设置后 `/v1` 接口强制鉴权。
- 网关不内置临时邮箱、自动注册、验证码识别等能力，只使用使用者自行录入的凭证。

## 插件接口

插件是普通可执行文件，网关通过 stdin/stdout 每行一个 JSON 通信：

```json
{"jsonrpc":"2.0","id":"p-1","method":"chat","params":{
  "model":"example-model","system":"",
  "messages":[{"role":"user","content":"hi"}],
  "max_tokens":1024,
  "credential":{"api_key":"..."}
}}
```

插件回推事件：

```json
{"jsonrpc":"2.0","id":"p-1","method":"event","params":{"type":"delta","text":"你"}}
{"jsonrpc":"2.0","id":"p-1","method":"event","params":{"type":"tool_call","tool_calls":[{"id":"c1","name":"search","arguments":"{\"q\":\"x\"}"}]}}
{"jsonrpc":"2.0","id":"p-1","method":"event","params":{"type":"usage","usage":{"prompt_tokens":12,"completion_tokens":5}}}
{"jsonrpc":"2.0","id":"p-1","method":"event","params":{"type":"done","finish":"stop"}}
```

- 必须实现 `models` 方法，网关启动时会先探测它（20 秒超时）。
- 每次 `chat` 必须以且仅以一个 `done` 或 `error` 结束。
- stdout 只能输出协议 JSON，日志一律写 stderr。
- 代理由网关统一注入，插件使用标准 HTTP 客户端即可。

完整字段、事件格式、工具闭环、超时与验收清单见 [docs/plugin-requirements.md](docs/plugin-requirements.md)。

## 目录结构

```
cmd/gateway/          入口：加载配置、拉起插件、启动 HTTP
internal/config/      YAML 配置
internal/credential/  凭证加密存储
internal/protocol/    三套协议的解析与渲染
internal/plugin/      插件 stdio 宿主与注册表
internal/gateway/     路由、限流、代理入口、HTTP 端点、控制台
configs/              网关配置
docs/                 插件规范
```

## 仓库边界

本仓库只提交网关本体，以下内容不会上传：

- 任何插件源码、二进制或插件配置（`plugins/`）；
- 凭证、`configs/settings.json`、`configs/credkey.txt` 等本地密钥；
- 运行日志、`bin/`、`*.exe`、缓存、`node_modules/`；
- 探针脚本与采集数据。

插件在独立仓库维护，通过 `configs/gateway.yaml` 接入。
