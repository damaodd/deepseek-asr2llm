// Command deepseek-asr2llm wires the LinkSoul Agent SDK (asr2llm) to the
// DeepSeek chat API: ASR text from the robot is sent to DeepSeek and the
// streamed reply is pushed back to the robot.
//
// 配置方式：默认读取当前目录下的 config.json（可复制 config.example.json 修改）。
// 也可用 -config 指定路径，或用环境变量覆盖配置文件中的值。
//
// 配置文件格式：
//
//	{
//	  "agibot": {
//	    "url": "wss://agentsdk.agibot.com/v1/agentsdk",
//	    "app_id": "app_xxx",
//	    "app_key": "ak_xxx",
//	    "app_secret": "sk_xxx"
//	  },
//	  "deepseek": {
//	    "base_url": "https://api.deepseek.com",
//	    "api_key": "sk-xxx",
//	    "model": "deepseek-flash",
//	    "system_prompt": "你是一个友好的语音助手...",
//	    "max_history": 20,
//	    "thinking": "disabled"
//	  }
//	}
//
// 环境变量（可选，优先级高于配置文件）：
// AGIBOT_URL / AGIBOT_APP_ID / AGIBOT_APP_KEY / AGIBOT_APP_SECRET
// DEEPSEEK_BASE_URL / DEEPSEEK_API_KEY / DEEPSEEK_MODEL
// DEEPSEEK_SYSTEM_PROMPT / DEEPSEEK_THINKING
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	agentsdk "github.com/agibot/linksoul-agentsdk"
	_ "github.com/agibot/linksoul-agentsdk/wsclient"
)

type authCb struct{}

func (authCb) OnAuthState(appID string, code int, msg string) {
	fmt.Printf("auth => %s %d %s\n", appID, code, msg)
}

// ---- configuration ----

type config struct {
	Agibot struct {
		URL       string `json:"url"`
		AppID     string `json:"app_id"`
		AppKey    string `json:"app_key"`
		AppSecret string `json:"app_secret"`
	} `json:"agibot"`
	Deepseek struct {
		BaseURL      string `json:"base_url"`
		APIKey       string `json:"api_key"`
		Model        string `json:"model"`
		SystemPrompt string `json:"system_prompt"` // 人设（system 提示词）
		MaxHistory   int    `json:"max_history"`   // 每台机器人保留的对话轮数
		Thinking     string `json:"thinking"`      // enabled / disabled
	} `json:"deepseek"`
}

func defaultConfig() *config {
	c := &config{}
	c.Agibot.URL = "wss://agentsdk.agibot.com/v1/agentsdk"
	c.Deepseek.BaseURL = "https://api.deepseek.com"
	c.Deepseek.Model = "deepseek-flash"
	c.Deepseek.SystemPrompt = "你是一个友好的语音助手，请用简短、口语化的中文回答。"
	c.Deepseek.MaxHistory = 20
	c.Deepseek.Thinking = "disabled"
	return c
}

func loadConfig(path string) (*config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := defaultConfig()
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	return c, nil
}

// overrideFromEnv applies optional environment-variable overrides on top of the
// config file values.
func overrideFromEnv(c *config) {
	if v := os.Getenv("AGIBOT_URL"); v != "" {
		c.Agibot.URL = v
	}
	if v := os.Getenv("AGIBOT_APP_ID"); v != "" {
		c.Agibot.AppID = v
	}
	if v := os.Getenv("AGIBOT_APP_KEY"); v != "" {
		c.Agibot.AppKey = v
	}
	if v := os.Getenv("AGIBOT_APP_SECRET"); v != "" {
		c.Agibot.AppSecret = v
	}
	if v := os.Getenv("DEEPSEEK_BASE_URL"); v != "" {
		c.Deepseek.BaseURL = v
	}
	if v := os.Getenv("DEEPSEEK_API_KEY"); v != "" {
		c.Deepseek.APIKey = v
	}
	if v := os.Getenv("DEEPSEEK_MODEL"); v != "" {
		c.Deepseek.Model = v
	}
	if v := os.Getenv("DEEPSEEK_SYSTEM_PROMPT"); v != "" {
		c.Deepseek.SystemPrompt = v
	}
	if v := os.Getenv("DEEPSEEK_THINKING"); v != "" {
		c.Deepseek.Thinking = v
	}
}

// ---- DeepSeek chat client (OpenAI-compatible, stdlib only) ----

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type thinking struct {
	Type string `json:"type"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Thinking thinking      `json:"thinking"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

type deepseekClient struct {
	baseURL  string
	apiKey   string
	model    string
	thinking string
	http     *http.Client
}

func newDeepseekClient(baseURL, apiKey, model, thinking string) *deepseekClient {
	if thinking != "enabled" {
		thinking = "disabled"
	}
	return &deepseekClient{
		baseURL:  strings.TrimRight(baseURL, "/"),
		apiKey:   apiKey,
		model:    model,
		thinking: thinking,
		http:     &http.Client{},
	}
}

// Chat streams a completion, invoking onDelta for every content chunk and
// returning the full reply text.
func (d *deepseekClient) Chat(messages []chatMessage, onDelta func(string)) (string, error) {
	body, err := json.Marshal(chatRequest{
		Model:    d.model,
		Messages: messages,
		Stream:   true,
		Thinking: thinking{Type: d.thinking},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, d.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.apiKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := d.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("deepseek http %d: %s", resp.StatusCode, string(b))
	}

	var sb strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024) // 允许单行大 token
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta.Content
		if delta == "" {
			continue
		}
		sb.WriteString(delta)
		if onDelta != nil {
			onDelta(delta)
		}
	}
	if err := scanner.Err(); err != nil {
		return sb.String(), err
	}
	return sb.String(), nil
}

// ---- per-agent conversation history ----

type chatHistory struct {
	mu         sync.Mutex
	byAgent    map[string][]chatMessage
	system     chatMessage
	maxHistory int
}

func newChatHistory(systemPrompt string, maxHistory int) *chatHistory {
	if maxHistory <= 0 {
		maxHistory = 20
	}
	return &chatHistory{
		byAgent:    map[string][]chatMessage{},
		system:     chatMessage{Role: "system", Content: systemPrompt},
		maxHistory: maxHistory,
	}
}

// appendUser records the user turn and returns the full message list
// (system + history + current user) for the LLM call.
func (h *chatHistory) appendUser(agentID, text string) []chatMessage {
	h.mu.Lock()
	defer h.mu.Unlock()

	hist := h.byAgent[agentID]
	hist = append(hist, chatMessage{Role: "user", Content: text})
	if len(hist) > h.maxHistory {
		hist = hist[len(hist)-h.maxHistory:]
	}
	h.byAgent[agentID] = hist

	msgs := make([]chatMessage, 0, len(hist)+1)
	msgs = append(msgs, h.system)
	msgs = append(msgs, hist...)
	return msgs
}

// remember records the assistant reply.
func (h *chatHistory) remember(agentID, content string) {
	if content == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	hist := h.byAgent[agentID]
	hist = append(hist, chatMessage{Role: "assistant", Content: content})
	if len(hist) > h.maxHistory {
		hist = hist[len(hist)-h.maxHistory:]
	}
	h.byAgent[agentID] = hist
}

func main() {
	configPath := flag.String("config", "config.json", "配置文件路径")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Printf("加载配置文件失败: %v\n", err)
		fmt.Println("请复制 config.example.json 为 config.json 并填写必填项。")
		os.Exit(1)
	}
	overrideFromEnv(cfg)

	if cfg.Agibot.AppID == "" || cfg.Agibot.AppKey == "" || cfg.Agibot.AppSecret == "" || cfg.Deepseek.APIKey == "" {
		fmt.Println("配置缺少必填项: agibot.app_id / agibot.app_key / agibot.app_secret / deepseek.api_key")
		os.Exit(1)
	}

	llm := newDeepseekClient(cfg.Deepseek.BaseURL, cfg.Deepseek.APIKey, cfg.Deepseek.Model, cfg.Deepseek.Thinking)
	history := newChatHistory(cfg.Deepseek.SystemPrompt, cfg.Deepseek.MaxHistory)

	sdk := agentsdk.Create(cfg.Agibot.URL, cfg.Agibot.AppID, cfg.Agibot.AppKey, cfg.Agibot.AppSecret)
	sdk.RegisterAuth(authCb{})

	cb := agentsdk.NewAsr2LlmCallback(sdk, func(agentID, eventID, text string, param *agentsdk.AgentParam, response *agentsdk.Asr2LlmResponse) {
		if text == "" { // 拒识：什么都不做
			return
		}
		fmt.Printf("[asr] agent=%s text=%q\n", agentID, text)

		// 先打断机器人当前输出，再流式下发 LLM 回复
		response.OnInterrupt(eventID, "chat", "")

		messages := history.appendUser(agentID, text)
		itemID := agentsdk.IdGenerator.GenerateItemID()

		full, err := llm.Chat(messages, func(delta string) {
			response.OnLlmItemDelta(eventID, itemID, delta)
		})
		if err != nil {
			fmt.Printf("[llm] error: %v\n", err)
			response.OnError(eventID, 500, err.Error())
			return
		}
		history.remember(agentID, full)

		response.OnLlmItemDone(eventID, itemID)
		response.OnLlmDone(eventID)
	})
	sdk.RegisterAsr2Llm(cb)

	sdk.Initialize()
	fmt.Println("deepseek-asr2llm started, waiting for messages...")
	select {} // 保持进程存活
}
