# deepseek-asr2llm

基于 [LinkSoul Agent SDK for Go](https://github.com/damaodd/linksoul-agentsdk-go) 的语音对话机器人示例：机器人通过 **asr2llm** 接入，将用户的语音转文字（ASR 文本）发送给 **DeepSeek** 大模型，再把流式回复推回机器人播放，实现真实的语音对话。

## 功能

- 接入 LinkSoul 网关的 `asr2llm` 通道（语音 → 文字 → LLM → 文字）
- 调用 DeepSeek Chat API，**流式**返回，边生成边下发（机器人边想边说）
- 打断机制：收到新指令先 `interrupt` 再回答
- 按机器人（agentId）维护多轮对话上下文（最多 20 条）
- 配置文件管理密钥，支持环境变量覆盖

## 前置条件

1. 一个 LinkSoul 应用，拿到 `appId` / `appKey` / `appSecret`（secret 至少 32 字节）。
2. 一个 [DeepSeek API Key](https://platform.deepseek.com/api_keys)（`sk-...`）。
3. Go 1.21+。

## 快速开始

```bash
# 1. 进入项目目录
cd deepseek-asr2llm

# 2. 复制配置模板
Copy-Item config.example.json config.json   # PowerShell
# 或 Linux/macOS: cp config.example.json config.json

# 3. 编辑 config.json，填入真实密钥
# 4. 运行
go run .
```

运行成功后出现：

```
auth => app_xxx 0 auth success
deepseek-asr2llm started, waiting for messages...
```

此时对机器人说话，就会得到 DeepSeek 的回复。

## 配置说明

### 配置文件 `config.json`

```json
{
  "agibot": {
    "url": "wss://agentsdk.agibot.com/v1/agentsdk",
    "app_id": "app_xxx",
    "app_key": "ak_xxx",
    "app_secret": "sk_xxx"
  },
  "deepseek": {
    "base_url": "https://api.deepseek.com",
    "api_key": "sk-xxx",
    "model": "deepseek-flash",
    "system_prompt": "你是一个友好的语音助手，请用简短、口语化的中文回答。",
    "max_history": 20,
    "thinking": "disabled"
  }
}
```

| 字段 | 说明 | 必填 |
| --- | --- | --- |
| `agibot.url` | LinkSoul 网关地址 | 否（有默认值）|
| `agibot.app_id` | 应用 ID | 是 |
| `agibot.app_key` | 应用 Key | 是 |
| `agibot.app_secret` | 应用密钥（≥32 字节）| 是 |
| `deepseek.base_url` | DeepSeek 接口地址 | 否（默认 `https://api.deepseek.com`）|
| `deepseek.api_key` | DeepSeek API Key | 是 |
| `deepseek.model` | 模型名 | 否（默认 `deepseek-flash`）|
| `deepseek.system_prompt` | **人设**（system 提示词）| 否（有默认值）|
| `deepseek.max_history` | 每台机器人保留的对话轮数 | 否（默认 `20`）|
| `deepseek.thinking` | 思考模式 `enabled` / `disabled` | 否（默认 `disabled`）|

可用模型：`deepseek-flash`、`deepseek-v4-pro`。

### 环境变量覆盖

所有字段都可用环境变量覆盖（优先级高于配置文件），适合临时切换密钥而不改文件：

| 环境变量 | 对应字段 |
| --- | --- |
| `AGIBOT_URL` | `agibot.url` |
| `AGIBOT_APP_ID` | `agibot.app_id` |
| `AGIBOT_APP_KEY` | `agibot.app_key` |
| `AGIBOT_APP_SECRET` | `agibot.app_secret` |
| `DEEPSEEK_BASE_URL` | `deepseek.base_url` |
| `DEEPSEEK_API_KEY` | `deepseek.api_key` |
| `DEEPSEEK_MODEL` | `deepseek.model` |
| `DEEPSEEK_SYSTEM_PROMPT` | `deepseek.system_prompt` |
| `DEEPSEEK_THINKING` | `deepseek.thinking` |

### 自定义配置文件路径

```bash
go run . -config ./my-config.json
```

## 思考模式

通过 `deepseek.thinking` 控制，默认 `disabled`（关闭），回复直接流式返回，语音场景更实时；设为 `enabled` 开启思考模式。

## 安全提示

- `config.json` 含真实密钥，已被 `.gitignore` 忽略，**切勿提交到仓库**。
- 若密钥不慎泄露，请立即到对应平台吊销/重置。

## 项目结构

```
deepseek-asr2llm/
├── main.go               # 主程序：SDK 接入 + DeepSeek 调用
├── config.example.json   # 配置模板
├── config.json           # 你的实际配置（已 gitignore）
├── go.mod / go.sum       # 依赖（通过 replace 指向本地 SDK）
└── .gitignore
```
