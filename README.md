# Wen CLI

一个基于命令行的AI助手工具，可以快速向AI模型提问并获取简洁的回答。

## 功能特点

- 从命令行直接向AI提问
- 支持OpenAI和Anthropic的API
- 配置简单，使用方便
- 获取简洁明了的回答

## 安装

### 1. 编译

```bash
git clone https://github.com/yourusername/wen.git
cd wen
go build -o wen
```

### 2. 安装到系统

```bash
sudo cp wen /usr/local/bin/
sudo cp wen.conf.example /etc/wen.conf
```

### 3. 配置

编辑配置文件，填入您的API密钥和其他配置:

```bash
sudo nano /etc/wen.conf
```

## 使用方法

直接在命令行中输入问题:

```bash
wen 如何解压tar文件？
```

```bash
wen 用Python如何读取JSON文件？
```

```bash
wen "解释Linux中的管道（pipe）机制"
```

## 配置文件

配置文件位于 `/etc/wen.conf`，包含以下设置:

```
# 使用的AI提供商 (openai 或 anthropic)
provider=openai

# 使用的模型
model=gpt-3.5-turbo

# API密钥
api_key=your_api_key_here

# API地址
api_url=https://api.openai.com/v1/chat/completions
```

## 支持的提供商

1. **OpenAI**
   - 默认API地址: https://api.openai.com/v1/chat/completions
   - 推荐模型: gpt-3.5-turbo, gpt-4

2. **Anthropic**
   - 默认API地址: https://api.anthropic.com/v1/messages
   - 推荐模型: claude-instant-1, claude-2

## 开发

要在本地开发，克隆仓库后运行:

```bash
go run main.go "你的问题"
```

## 许可证

MIT 