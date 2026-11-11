# 站点插件规范

本仓库只包含网关核心（协议转换、路由、限流、凭证存储、控制台）。站点插件**不在本仓库分发**，由使用者按本规范独立实现并配置接入。

任何可执行程序，只要能启动、能按下面的协议在 stdin/stdout 上通信，就可以作为插件接入网关，与实现语言无关。

---

## 1. 进程模型

- 每个站点对应一个常驻子进程。网关按配置中的 `command`、`args`、`env`、`dir` 启动它。
- 子进程通过 **stdin 接收请求**，通过 **stdout 返回响应和流式事件**。
- **stderr 用于日志**，网关会把 stderr 转发到自己的日志中。
- 插件启动后必须常驻，不能处理完一个请求就退出。
- 网关退出时会终止插件进程；插件必须及时退出，不得留下孤儿进程或后台线程。
- 插件不得要求交互式输入，不得依赖当前工作目录，不得监听端口。

## 2. 帧格式

- 编码统一为 UTF-8。
- **每行一个完整 JSON 对象**（NDJSON），以 `\n` 结束，不允许跨行 JSON。
- stdout **只能**输出协议消息；任何调试输出、进度条、警告都必须写到 stderr。
- 单行上限 16 MiB；超过会被截断并导致解析失败。
- 每条消息写完必须立即 flush，尤其是流式事件，不能攒批到最后。
- 建议对 stdin/stdout 关闭缓冲或使用行缓冲，避免死锁。

## 3. 方法

网关目前调用两个方法：`models` 和 `chat`。`ping` 为可选方法，网关不强制要求。

### 3.1 models

网关在启动插件时调用，超时 **20 秒**。超时或返回错误会让该插件被标记为不可用，且不参与路由。

请求：

```json
{"jsonrpc":"2.0","id":"p-1","method":"models","params":{}}
```

响应：

```json
{
  "jsonrpc": "2.0",
  "id": "p-1",
  "result": {
    "models": [
      {
        "id": "openai-fast",
        "name": "OpenAI Fast",
        "capabilities": ["streaming", "tools"],
        "context_length": 32768
      }
    ]
  }
}
```

要求：

- `result.models` 必须是数组，不能为空（确实没有可用模型时应视为站点不可用）。
- `id` 必填且稳定，建议与上游模型名一致；`name`、`capabilities`、`context_length` 可选。
- 配置里的 `models` 列表同样会建立模型索引，**两者应保持一致**，否则会出现「模型目录里有、实际却路由不到」。
- 多个插件可以声明同一个模型 ID，网关会按 `priority` 组成故障转移链。

### 3.2 chat

请求：

```json
{
  "jsonrpc": "2.0",
  "id": "p-2",
  "method": "chat",
  "params": {
    "model": "openai-fast",
    "system": "",
    "messages": [
      {"role": "user", "content": "你好"}
    ],
    "tools": [
      {"type": "function", "name": "search", "description": "搜索", "parameters": {"type": "object", "properties": {"q": {"type": "string"}}}}
    ],
    "tool_choice": "auto",
    "max_tokens": 1024,
    "temperature": 0.7,
    "top_p": 1,
    "stop": ["\n\n"],
    "builtin_tools": ["web_search"],
    "credential": {"api_key": "sk-xxx"}
  }
}
```

字段说明：

| 字段 | 类型 | 说明 |
|---|---|---|
| `model` | string | 客户端请求的模型 ID，已在配置或 `models` 中声明 |
| `system` | string | 系统提示词，可能为空 |
| `messages` | array | 统一消息列表，见下表 |
| `tools` | array | 函数工具定义；仅 `tools: native` 的站点会收到 |
| `tool_choice` | any | 客户端原始值，可能是 `"auto"` 或对象 |
| `max_tokens` | number | 最大输出 token 数，可能为 0（表示未指定） |
| `temperature` | number | 采样温度，可能为 0 |
| `top_p` | number | 核采样，可能为 0 |
| `stop` | array | 停止词列表 |
| `builtin_tools` | array | 平台内置工具名；仅 `tools: builtin` 的站点会收到 |
| `credential` | object | 控制台录入的凭证，键名见第 6 节 |

`messages` 元素结构：

```json
{"role":"user","content":"文本"}
{"role":"assistant","content":"","tool_calls":[{"id":"call_1","name":"search","arguments":"{\"q\":\"x\"}"}]}
{"role":"tool","content":"工具结果","tool_call_id":"call_1","name":"search"}
```

- `role` 取值：`system`、`user`、`assistant`、`tool`。
- `content` 是已经展平的纯文本；多模态内容网关会在解析阶段压平成文本。
- `arguments` 必须是 JSON **字符串**，不是对象。
- 工具结果消息通过 `tool_call_id` 关联到助手的工具调用。

注意：**`chat` 参数中没有 `stream` 字段**。网关始终按流式方式读取插件输出，非流式响应由网关自行聚合。插件应始终以事件流形式返回结果。

### 3.3 ping（可选）

```json
{"jsonrpc":"2.0","id":"p-1","method":"ping","params":{}}
```

返回 `{"jsonrpc":"2.0","id":"p-1","result":{"pong":true}}` 即可。

## 4. 流式事件

插件通过 `method: "event"` 的帧推送事件，`id` 必须与对应 `chat` 请求的 `id` 完全一致：

```json
{"jsonrpc":"2.0","id":"p-2","method":"event","params":{"type":"delta","text":"你"}}
{"jsonrpc":"2.0","id":"p-2","method":"event","params":{"type":"delta","text":"好"}}
{"jsonrpc":"2.0","id":"p-2","method":"event","params":{"type":"usage","usage":{"prompt_tokens":12,"completion_tokens":5}}}
{"jsonrpc":"2.0","id":"p-2","method":"event","params":{"type":"done","finish":"stop"}}
```

事件类型：

| type | 字段 | 说明 |
|---|---|---|
| `delta` | `text` | 增量文本，可多次发送，顺序拼接 |
| `tool_call` | `tool_calls` | 工具调用，见第 5 节 |
| `usage` | `usage.prompt_tokens`、`usage.completion_tokens` | token 用量，建议在 `done` 前发送一次 |
| `done` | `finish` | 本次调用正常结束，`finish` 取值 `stop` / `length` / `tool_calls` |
| `error` | `error` | 本次调用失败，`error` 为可读错误信息 |

硬性要求：

1. **每次 `chat` 必须以且仅以一个 `done` 或一个 `error` 结束。**
2. `done` / `error` 之后不得再发送任何该请求的事件。
3. `finish` 为 `tool_calls` 时，必须已经发送过完整的 `tool_call`。
4. 发生错误时不要发送 `done`；`error` 后直接停止。
5. 错误信息中可以包含上游状态码和简短原因，**不得包含凭证、Cookie、完整请求头**。
6. 单个事件中的文本不要过大；建议按上游分片原样转发，便于网关边收边渲染。

### 4.1 非流式最终应答（兼容形态）

除事件流外，插件也可以直接返回一个 `result`，由网关转换成事件：

```json
{"jsonrpc":"2.0","id":"p-2","result":{"content":"完整回复","usage":{"prompt_tokens":12,"completion_tokens":5},"finish":"stop"}}
```

或返回事件数组：

```json
{"jsonrpc":"2.0","id":"p-2","result":[{"type":"delta","text":"完整回复"},{"type":"done","finish":"stop"}]}
```

这种方式只适合无法流式输出的站点。能流式时优先使用事件流，否则首字延迟和超时都会变差。

## 5. 工具调用

当插件声明 `tools: native` 时，必须实现完整的工具调用闭环：

1. 第一轮：把 `tools` 按上游要求的格式透传，把模型返回的工具调用整理成：

```json
{
  "jsonrpc": "2.0",
  "id": "p-2",
  "method": "event",
  "params": {
    "type": "tool_call",
    "tool_calls": [
      {"id": "call_1", "name": "search", "arguments": "{\"q\":\"x\"}"}
    ]
  }
}
```

2. 然后发送 `{"type":"done","finish":"tool_calls"}`。
3. 第二轮：网关会把助手工具调用与 `role: tool` 的结果重新发回来，插件需要把工具结果交给上游并继续生成最终回答。

要求：

- **一个工具调用只发一次完整事件**，`id`、`name`、`arguments` 必须齐全；不要在后续 chunk 里重复发送同一个调用，也不要只发增量参数。
- `arguments` 必须是合法 JSON 字符串；无法解析时应发送 `error`，不要输出半截 JSON。
- `tools: none` / `builtin` 的插件不会收到函数工具请求，不应自行暴露自定义工具 schema。
- `builtin` 档位通过 `builtin_tools` 收到平台内置工具名，只需在请求中体现相应能力。

## 6. 凭证与代理

### 6.1 credential

`credential` 只包含插件在配置中声明的字段：

```yaml
auth:
  type: api_key
  required_fields: [api_key]
  optional_fields: [csrf_token]
```

- 必填字段缺失时，网关不会把请求路由到该插件。
- 多行值（每行一个 key）由网关按调用轮换，插件每次只会收到一个值。
- 插件**不得**要求未声明的字段，不得自行读取网关的凭证文件或环境变量。
- 凭证只用于本次上游请求，不得落盘、不得打日志、不得转发到第三方。

### 6.2 代理（网关统一注入，插件不要自己实现）

网关启动插件进程时会自动注入以下环境变量，指向网关内置的**轮换代理入口**：

```
HTTP_PROXY / HTTPS_PROXY / ALL_PROXY
http_proxy / https_proxy / all_proxy
NO_PROXY / no_proxy = 127.0.0.1,localhost,::1
```

该入口从控制台的全局代理池里按请求轮换出口，池为空时直连。因此：

- 插件**只需要使用标准 HTTP 客户端**（Go 的默认 `http.Transport`、Python `requests`/`urllib`、`curl` 等），不要自己实现代理选择、轮换、代理池配置或读取 `credential.proxy`。
- 不要覆盖或删除网关注入的 `*_PROXY` 环境变量；如需访问本机服务，使用 `NO_PROXY`（已默认包含回环地址）。
- 如果所用客户端默认不读取代理环境变量（例如 Node 原生 `fetch`/undici），请在插件内显式启用环境代理（如 `EnvHttpProxyAgent`），但仍不要自行选择出口。
- 旧版网关可能仍会注入 `credential.proxy` 作为兼容字段；新插件可以忽略它。**不要同时使用环境代理和 `credential.proxy`**，避免双重代理。
- 代理不可用时应返回 `error`，不要静默改直连。
- 不要在插件配置里写死代理地址；需要调整出口时，只改控制台的代理池。

## 7. 超时、并发与限流

- 网关对单次请求有超时（默认 180 秒，可按配置调整）。插件必须在超时前发送 `done` 或 `error`。
- 插件内部也要设置连接/读取超时；不得无限等待上游。
- 遇到上游 429 / 5xx：允许有限次退避重试（建议 1–2 次），仍失败则发送 `error`。
- 同一插件可能同时收到多个 `chat` 请求（默认并发上限 2，可按配置调整）。插件必须保证并发安全，或在内部排队串行处理。
- 网关已经做令牌桶限流；插件仍应按站点真实配额做进一步节流，避免被封禁。
- 任何未捕获异常都不能让进程退出；解析失败的行应返回 JSON-RPC 错误或忽略，而不是崩溃。

## 8. 安全与合规

- 不收集、不上报任何使用数据；日志中不得出现凭证、Cookie、完整请求体。
- 只读写自己的目录，不修改网关的 `configs/`、凭证文件或数据库。
- 错误信息面向使用者，保持简洁；不要把上游原始响应头（可能含 Cookie）整个抛给客户端。

## 9. 配置接入

在网关 `configs/gateway.yaml` 中声明：

```yaml
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

字段说明：

| 字段 | 说明 |
|---|---|
| `name` | 全局唯一，控制台与日志中显示的名称 |
| `command` | 可执行文件路径；相对路径按网关启动目录解析 |
| `args` | 启动参数数组，可选 |
| `env` | 传给插件的环境变量，可选；插件不要依赖未声明的环境变量 |
| `dir` | 插件工作目录，可选 |
| `priority` | 数字越小越优先；同模型多来源时按此顺序故障转移 |
| `tools` | `native` / `builtin` / `none`，必须与实际能力一致 |
| `timeout` | 单次请求超时，缺省跟随全局 |
| `per_plugin_rps` | 插件级限流，覆盖全局值 |
| `models` | 该插件提供的模型 ID 列表，必须包含 `models` 方法返回的 ID |
| `auth.type` | `none` / `api_key` / `bearer` / `cookie` / `oauth` |
| `auth.required_fields` | 缺失则插件不参与路由 |
| `auth.optional_fields` | 可选凭证字段 |
| `auth.quota` | 额度计量方式：`ip` 按出口 IP（账号数=代理数）、`key` 按 key 数；留空则 `auth.type: none` 按 IP，其余按 key |

同一个模型 ID 被多个插件声明时，会形成故障转移链；带函数工具的请求只会路由到 `tools: native` 的插件。

## 10. 交付检查清单

- [ ] 启动后 20 秒内正确响应 `models`。
- [ ] stdout 只有合法 JSON-RPC 行，日志全部在 stderr。
- [ ] `chat` 以且仅以一个 `done` / `error` 结束，之后无输出。
- [ ] 流式文本按 `delta` 连续输出，事件及时 flush。
- [ ] `tools: native` 时工具调用字段完整，`finish` 为 `tool_calls`，支持第二轮工具结果。
- [ ] 使用标准 HTTP 客户端，能自动使用网关注入的 `HTTP_PROXY`；没有自己实现代理轮换。
- [ ] 代理池为空时直连，代理不可用时返回可读 `error`。
- [ ] 并发 2 个 `chat` 不串流、不崩溃。
- [ ] 上游超时 / 429 / 5xx 能返回可读 `error`，进程不退出。
- [ ] 日志与错误信息中无凭证、Cookie、完整请求头。
- [ ] 收到终止信号后能及时退出，无残留进程。
- [ ] README 写清构建方式、依赖的 `env`、所需凭证字段和速率限制。

## 11. 完整交互示例

启动探测：

```text
→ {"jsonrpc":"2.0","id":"p-1","method":"models","params":{}}
← {"jsonrpc":"2.0","id":"p-1","result":{"models":[{"id":"example-model","name":"Example"}]}}
```

一次普通对话：

```text
→ {"jsonrpc":"2.0","id":"p-2","method":"chat","params":{"model":"example-model","system":"","messages":[{"role":"user","content":"hi"}],"max_tokens":256,"credential":{"api_key":"sk-xxx"}}}
← {"jsonrpc":"2.0","id":"p-2","method":"event","params":{"type":"delta","text":"你"}}
← {"jsonrpc":"2.0","id":"p-2","method":"event","params":{"type":"delta","text":"好"}}
← {"jsonrpc":"2.0","id":"p-2","method":"event","params":{"type":"usage","usage":{"prompt_tokens":3,"completion_tokens":2}}}
← {"jsonrpc":"2.0","id":"p-2","method":"event","params":{"type":"done","finish":"stop"}}
```

一次工具调用：

```text
→ {"jsonrpc":"2.0","id":"p-3","method":"chat","params":{"model":"example-model","messages":[{"role":"user","content":"查一下天气"}],"tools":[{"type":"function","name":"weather","description":"查询天气","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}]}}
← {"jsonrpc":"2.0","id":"p-3","method":"event","params":{"type":"tool_call","tool_calls":[{"id":"call_1","name":"weather","arguments":"{\"city\":\"上海\"}"}]}}
← {"jsonrpc":"2.0","id":"p-3","method":"event","params":{"type":"done","finish":"tool_calls"}}
```

## 12. 常见错误

| 现象 | 原因 |
|---|---|
| 插件启动失败 / 模型为空 | `models` 未响应、响应超时、返回结构不是 `{"models":[...]}` |
| 请求一直挂起 | 没有发送 `done` / `error`，或没有 flush stdout |
| 流式内容重复 | `done` 之后仍在发事件，或事件 `id` 与请求 `id` 不一致 |
| 输出被忽略 | 往 stdout 打了日志 / 多行 JSON / 非 UTF-8 |
| 工具调用失效 | `arguments` 不是 JSON 字符串，或分多帧发送同一调用 |
| 站点调用全部超时 | 插件没有使用标准客户端/环境代理，或代理池里的出口不可用 |
| 平台偶发卡死 | 插件并发处理不安全，或上游无超时无限等待 |
| 插件被判定未就绪 | 必填凭证未配置，或 `config.models` 与 `models` 返回值不一致 |
