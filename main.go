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
	Model          string `json:"model"`
	APIKey         string `json:"api_key"`
	APIURL         string `json:"api_url"`
	Provider       string `json:"provider"` // "openai", "anthropic", etc.
	PromptTemplate string `json:"prompt_template"`
	Stream         bool   `json:"stream"`   // Whether to use streaming API
}

// Default prompt template
const defaultPromptTemplate = "回答用户问题，务必做到简洁，不要有任何废话。输出纯文本格式(NO MARKDOWN)，适合在终端显示。"
const promptForTerminal = "使用以下格式添加颜色和样式：<red>红色文本</red>、<green>绿色文本</green>、<blue>蓝色文本</blue>、<bold>粗体文本</bold>、<yellow>黄色文本</yellow>。重要内容请使用颜色或粗体突出显示。"

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

	startTime := time.Now()
	var answer string
	var err2 error

	// Use streaming or non-streaming API based on config
	if config.Stream {
		answer, err2 = streamAI(question, config)
	} else {
		answer, err2 = askAI(question, config)
	}

	if err2 != nil {
		fmt.Printf("请求AI失败: %v\n", err2)
		os.Exit(1)
	}

	// Only print the answer if not streaming (streaming already prints)
	if !config.Stream {
		// Process and print the answer with terminal formatting
		formattedAnswer := processTerminalFormatting(answer)
		fmt.Println(formattedAnswer)
	}

	elapsedTime := time.Since(startTime).Seconds()
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
		Model:          "gpt-3.5-turbo",
		APIURL:         "https://api.openai.com/v1/chat/completions",
		Provider:       "openai",
		PromptTemplate: defaultPromptTemplate,
		Stream:         true, // Default to non-streaming
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
		case "stream":
			config.Stream = strings.ToLower(value) == "true" || value == "1"
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
func askAI(question string, config *Config) (string, error) {
	var requestBody []byte
	var err error

	// 在非流式模式下，添加终端格式化提示
	if !config.Stream {
		config.PromptTemplate = config.PromptTemplate + " " + promptForTerminal
	}

	switch config.Provider {
	case "openai":
		requestBody, err = createOpenAIRequest(question, config, false)
	case "anthropic":
		requestBody, err = createAnthropicRequest(question, config, false)
	default:
		requestBody, err = createOpenAIRequest(question, config, false) // Default to OpenAI
	}

	if err != nil {
		return "", err
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", config.APIURL, bytes.NewBuffer(requestBody))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+config.APIKey)

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API返回错误: %s", string(body))
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
		return "", err
	}

	return answer, nil
}

// streamAI sends the question to the AI API and streams the response
func streamAI(question string, config *Config) (string, error) {
	var requestBody []byte
	var err error

	switch config.Provider {
	case "openai":
		requestBody, err = createOpenAIRequest(question, config, true)
	case "anthropic":
		requestBody, err = createAnthropicRequest(question, config, true)
	default:
		requestBody, err = createOpenAIRequest(question, config, true) // Default to OpenAI
	}

	if err != nil {
		return "", err
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", config.APIURL, bytes.NewBuffer(requestBody))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+config.APIKey)
	req.Header.Set("Accept", "text/event-stream")

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API返回错误: %s", string(body))
	}

	// Process streaming response based on provider
	var fullResponse string
	switch config.Provider {
	case "anthropic":
		fullResponse, err = processAnthropicStream(resp.Body)
	default: // Default to OpenAI
		fullResponse, err = processOpenAIStream(resp.Body)
	}

	if err != nil {
		return "", err
	}

	return fullResponse, nil
}

// createOpenAIRequest creates the request body for OpenAI API
func createOpenAIRequest(question string, config *Config, stream bool) ([]byte, error) {
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
		"stream": stream,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}
	
	// 调试打印
	fmt.Println("\n\033[1m发送给 OpenAI 的内容:\033[0m")
	fmt.Printf("系统提示: %s\n", config.PromptTemplate)
	fmt.Printf("用户问题: %s\n", question)
	fmt.Println()
	
	return jsonData, nil
}

// createAnthropicRequest creates the request body for Anthropic API
func createAnthropicRequest(question string, config *Config, stream bool) ([]byte, error) {
	requestBody := map[string]interface{}{
		"model": config.Model,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": question,
			},
		},
		"system": config.PromptTemplate,
		"stream": stream,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}
	
	// 调试打印
	fmt.Println("\n\033[1m发送给 Anthropic 的内容:\033[0m")
	fmt.Printf("系统提示: %s\n", config.PromptTemplate)
	fmt.Printf("用户问题: %s\n", question)
	fmt.Println()
	
	return jsonData, nil
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

// processOpenAIStream processes the streaming response from OpenAI API
func processOpenAIStream(responseBody io.Reader) (string, error) {
	scanner := bufio.NewScanner(responseBody)
	var fullResponse string
	
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		
		// Skip the "data: " prefix
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			
			// Check for the end of the stream
			if data == "[DONE]" {
				break
			}
			
			// Parse the JSON data
			var streamResponse struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			
			if err := json.Unmarshal([]byte(data), &streamResponse); err != nil {
				continue // Skip malformed data
			}
			
			// Extract and print the content
			if len(streamResponse.Choices) > 0 {
				content := streamResponse.Choices[0].Delta.Content
				if content != "" {
					formattedContent := processTerminalFormatting(content)
					fmt.Print(formattedContent)
					fullResponse += content
				}
			}
		}
	}
	
	if err := scanner.Err(); err != nil {
		return fullResponse, fmt.Errorf("读取流式响应失败: %w", err)
	}
	
	return fullResponse, nil
}

// processAnthropicStream processes the streaming response from Anthropic API
func processAnthropicStream(responseBody io.Reader) (string, error) {
	scanner := bufio.NewScanner(responseBody)
	var fullResponse string
	
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || !strings.HasPrefix(line, "data: ") {
			continue
		}
		
		// Skip the "data: " prefix
		data := strings.TrimPrefix(line, "data: ")
		
		// Check for the end of the stream
		if data == "[DONE]" {
			break
		}
		
		// Parse the JSON data
		var streamResponse struct {
			Type    string `json:"type"`
			Delta   struct {
				Text string `json:"text"`
			} `json:"delta"`
		}
		
		if err := json.Unmarshal([]byte(data), &streamResponse); err != nil {
			continue // Skip malformed data
		}
		
		// Extract and print the content
		if streamResponse.Type == "content_block_delta" && streamResponse.Delta.Text != "" {
			formattedContent := processTerminalFormatting(streamResponse.Delta.Text)
			fmt.Print(formattedContent)
			fullResponse += streamResponse.Delta.Text
		}
	}
	
	if err := scanner.Err(); err != nil {
		return fullResponse, fmt.Errorf("读取流式响应失败: %w", err)
	}
	
	return fullResponse, nil
}
