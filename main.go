package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"regexp"
)

// Config holds the configuration from /etc/wen.conf
type Config struct {
	Model         string `json:"model"`
	APIKey        string `json:"api_key"`
	APIURL        string `json:"api_url"`
	Provider      string `json:"provider"` // "openai", "anthropic", etc.
	PromptTemplate string `json:"prompt_template"`
}

// Default prompt template
const defaultPromptTemplate = "回答用户问题，务必做到简洁，不要有任何废话。输出纯文本格式(NO MARKDOWN)，适合在终端显示。使用以下格式添加颜色和样式：<red>红色文本</red>、<green>绿色文本</green>、<blue>蓝色文本</blue>、<bold>粗体文本</bold>、<yellow>黄色文本</yellow>。重要内容请使用颜色或粗体突出显示。"

// processTerminalFormatting converts custom format tags to ANSI escape sequences
func processTerminalFormatting(text string) string {
	// Define replacements for custom tags
	replacements := map[string]string{
		"<red>":    "\033[31m",
		"</red>":   "\033[0m",
		"<green>":  "\033[32m",
		"</green>": "\033[0m",
		"<blue>":   "\033[34m",
		"</blue>":  "\033[0m",
		"<bold>":   "\033[1m",
		"</bold>":  "\033[0m",
		"<yellow>": "\033[33m",
		"</yellow>": "\033[0m",
	}
	
	result := text
	for tag, ansi := range replacements {
		result = strings.ReplaceAll(result, tag, ansi)
	}
	
	// Also handle any raw escape sequences that might be in the text
	// Convert \e to the actual escape character
	re := regexp.MustCompile(`\\e\[(\d+)m`)
	result = re.ReplaceAllString(result, "\033[$1m")
	
	return result
}

func main() {
	// Check if arguments are provided
	if len(os.Args) < 2 {
		fmt.Println("使用方式: ./wen <问题>")
		os.Exit(1)
	}

	// Load configuration
	config, err := loadConfig("/etc/wen.conf")
	if err != nil {
		// Try to load from local test.conf if /etc/wen.conf is not available
		config, err = loadConfig("./test.conf")
		if err != nil {
			fmt.Printf("无法加载配置文件: %v\n", err)
			os.Exit(1)
		}
	}

	// Get the user question by joining all arguments
	question := strings.Join(os.Args[1:], " ")

	// Ask the AI model
	answer, elapsedTime, err := askAI(question, config)
	if err != nil {
		fmt.Printf("请求AI失败: %v\n", err)
		os.Exit(1)
	}

	// Process and print the answer with terminal formatting
	formattedAnswer := processTerminalFormatting(answer)
	fmt.Println(formattedAnswer)
	fmt.Printf("\n\033[1m耗时: %.2f 秒\033[0m\n", elapsedTime)
}

// loadConfig reads and parses the configuration file
func loadConfig(configPath string) (*Config, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("打开配置文件失败: %w", err)
	}
	defer file.Close()

	config := &Config{
		// Default values
		Model:         "gpt-3.5-turbo",
		APIURL:        "https://api.openai.com/v1/chat/completions",
		Provider:      "openai",
		PromptTemplate: defaultPromptTemplate,
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		// Skip comments and empty lines
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "model":
			config.Model = value
		case "api_key":
			config.APIKey = value
		case "api_url":
			config.APIURL = value
		case "provider":
			config.Provider = value
		case "prompt_template":
			config.PromptTemplate = value
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	if config.APIKey == "" {
		return nil, fmt.Errorf("配置文件中缺少 api_key")
	}

	return config, nil
}

// askAI sends the question to the AI API and returns the answer
func askAI(question string, config *Config) (string, float64, error) {
	startTime := time.Now()
	
	var requestBody []byte
	var err error

	switch config.Provider {
	case "openai":
		requestBody, err = createOpenAIRequest(question, config)
	case "anthropic":
		requestBody, err = createAnthropicRequest(question, config)
	default:
		requestBody, err = createOpenAIRequest(question, config) // Default to OpenAI
	}

	if err != nil {
		return "", 0, err
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", config.APIURL, bytes.NewBuffer(requestBody))
	if err != nil {
		return "", 0, fmt.Errorf("创建请求失败: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+config.APIKey)

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("API返回错误: %s", string(body))
	}

	// Parse response based on provider
	var answer string
	switch config.Provider {
	case "anthropic":
		answer, err = parseAnthropicResponse(body)
	default: // Default to OpenAI
		answer, err = parseOpenAIResponse(body)
	}
	
	if err != nil {
		return "", 0, err
	}
	
	elapsedTime := time.Since(startTime).Seconds()
	return answer, elapsedTime, nil
}

// createOpenAIRequest creates the request body for OpenAI API
func createOpenAIRequest(question string, config *Config) ([]byte, error) {
	requestBody := map[string]interface{}{
		"model": config.Model,
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": config.PromptTemplate,
			},
			{
				"role":    "user",
				"content": question,
			},
		},
	}

	return json.Marshal(requestBody)
}

// createAnthropicRequest creates the request body for Anthropic API
func createAnthropicRequest(question string, config *Config) ([]byte, error) {
	requestBody := map[string]interface{}{
		"model": config.Model,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": question,
			},
		},
		"system": config.PromptTemplate,
	}

	return json.Marshal(requestBody)
}

// parseOpenAIResponse parses the response from OpenAI API
func parseOpenAIResponse(responseBody []byte) (string, error) {
	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("API返回了空的响应")
	}

	return response.Choices[0].Message.Content, nil
}

// parseAnthropicResponse parses the response from Anthropic API
func parseAnthropicResponse(responseBody []byte) (string, error) {
	var response struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	if len(response.Content) == 0 {
		return "", fmt.Errorf("API返回了空的响应")
	}

	return response.Content[0].Text, nil
}
