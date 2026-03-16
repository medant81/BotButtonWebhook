package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
)

type BotButtonWebhookPlugin struct {
	plugin.MattermostPlugin
	router      *mux.Router
	botWebhooks map[string]string
}

func (p *BotButtonWebhookPlugin) OnActivate() error {

	// TODO Для прода убрать лишние регистрации, влогах убрать тег meda-plugin
	// Регистрируем обработчик для всех action-запросов
	p.API.LogInfo("meda-plugin: === OnActivate called ===")

	p.router = mux.NewRouter()
	//p.router.HandleFunc("/actions/{action_id:[a-zA-Z0-9_-]+}", p.handleButtonClick).Methods("POST")
	p.router.HandleFunc("/actions/{action_id}", p.handleButtonClick).Methods("POST")
	p.router.HandleFunc("/actions", p.handleButtonClick).Methods("POST")
	p.router.HandleFunc("/actions/message", p.handleButtonClick).Methods("POST")

	// Важно: обрабатываем также запросы без action_id для диагностики
	//p.router.HandleFunc("/actions", p.handleButtonClick).Methods("POST")

	p.router.HandleFunc("", func(w http.ResponseWriter, r *http.Request) {
		p.API.LogInfo("meda-plugin:Root endpoint called", "path", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Bot Button Webhook Plugin Root"))
	}).Methods("GET")

	// Обработчик для проверки работоспособности
	p.router.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("pong"))
	}).Methods("GET")

	// Обработчик для любых других путей (для отладки)
	p.router.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.API.LogInfo("meda-plugin:Catch-all handler",
			"path", r.URL.Path,
			"method", r.Method,
			"query", r.URL.RawQuery)
		http.NotFound(w, r)
	})

	// Добавим middleware для логирования всех запросов
	p.router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p.API.LogInfo("meda-plugin: Router middleware",
				"path", r.URL.Path,
				"method", r.Method,
				"content-type", r.Header.Get("Content-Type"))
			next.ServeHTTP(w, r)
		})
	})

	p.loadConfiguration() // Загружаем webhook'и при активации
	p.API.LogInfo("meda-plugin: Bot Button Webhook plugin activated successfully",
		"routes", "registered",
		"plugin_id", "bot-button-webhook")

	return nil
}

// MessageHasBeenPosted вызывается Mattermost после создания нового поста.
// Логика перенесена из mattermost-plugin-bot-webhook-2-main, но используется SDK из "server/public".
func (p *BotButtonWebhookPlugin) MessageHasBeenPosted(c *plugin.Context, post *model.Post) {
	// Гарантируем, что конфигурация загружена
	if p.botWebhooks == nil {
		p.loadConfiguration()
	}

	if len(p.botWebhooks) == 0 {
		return
	}

	channel, appErr := p.API.GetChannel(post.ChannelId)
	if appErr != nil {
		p.API.LogError("meda-plugin: Failed to get channel", "error", appErr.Error())
		return
	}

	// Игнорируем сообщения, отправленные самим ботом
	if _, isBot := p.botWebhooks[post.UserId]; isBot {
		return
	}

	// Эвристика: в DM-канале имя канала содержит user_id участника.
	// Определяем, какому боту написали, по списку bot_id из настроек.
	var botID string
	var webhookURL string
	for configuredBotID, url := range p.botWebhooks {
		if configuredBotID == "" || url == "" {
			continue
		}
		if strings.Contains(channel.Name, configuredBotID) {
			botID = configuredBotID
			webhookURL = url
			break
		}
	}

	if botID == "" || webhookURL == "" {
		return
	}

	p.API.LogDebug("meda-plugin: Message to bot detected",
		"channel", channel.Name,
		"user", post.UserId,
		"message", post.Message,
		"bot_id", botID,
	)

	jsonPayload, marshalErr := json.Marshal(post)
	if marshalErr != nil {
		p.API.LogError("meda-plugin: Failed to marshal JSON payload", "error", marshalErr.Error())
		return
	}

	req, reqErr := http.NewRequest("POST", webhookURL, bytes.NewBuffer(jsonPayload))
	if reqErr != nil {
		p.API.LogError("meda-plugin: Failed to create HTTP request", "error", reqErr.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, doErr := client.Do(req)
	if doErr != nil {
		p.API.LogError("meda-plugin: Failed to make an HTTP request", "error", doErr.Error())
		return
	}
	defer resp.Body.Close()
}

// ServeHTTP вызывается Mattermost для всех HTTP-запросов к плагину (/plugins/bot-button-webhook/...).
// Сигнатура с *plugin.Context обязательна — без неё Mattermost не регистрирует метод как хук.
func (p *BotButtonWebhookPlugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	p.API.LogInfo("meda-plugin: === ServeHTTP called ===",
		"path", r.URL.Path,
		"method", r.Method,
		"remote_addr", r.RemoteAddr)

	if p.router == nil {
		p.API.LogError("meda-plugin: router is nil")
		http.Error(w, "Plugin not properly initialized", http.StatusInternalServerError)
		return
	}

	p.router.ServeHTTP(w, r)
}

// OnConfigurationChange вызывается Mattermost автоматически при каждом сохранении
// настроек плагина в System Console. Перезагружает map ботов без рестарта плагина.
func (p *BotButtonWebhookPlugin) OnConfigurationChange() error {
	p.loadConfiguration()
	return nil
}

// Загрузка конфигурации (список ботов и их webhook)
func (p *BotButtonWebhookPlugin) loadConfiguration() {
	config := p.getConfiguration()
	p.botWebhooks = make(map[string]string)

	if config.BotWebhooks == "" {
		p.API.LogWarn("meda-plugin: No bot webhooks configured")
		return
	}

	// Пример строки из настроек: "bot_id1: https://url1.com, bot_id2: https://url2.com"
	pairs := strings.Split(config.BotWebhooks, ",")
	for _, pair := range pairs {
		parts := strings.SplitN(strings.TrimSpace(pair), ":", 2)
		if len(parts) == 2 {
			botID := strings.TrimSpace(parts[0])
			url := strings.TrimSpace(parts[1])
			p.botWebhooks[botID] = url
			p.API.LogInfo("meda-plugin: Added webhook for bot", "bot_id", botID, "url", url)
		}
	}
}

// Конфигурация
type Configuration struct {
	BotWebhooks string `json:"BotWebhooks"`
}

func (p *BotButtonWebhookPlugin) getConfiguration() *Configuration {
	var config Configuration
	if err := p.API.LoadPluginConfiguration(&config); err != nil {
		p.API.LogError("meda-plugin: Failed to load config", "err", err.Error())
		return &Configuration{}
	}
	return &config
}

// Структура для interactive message action
type InteractiveMessagePayload struct {
	UserId      string                 `json:"user_id"`
	UserName    string                 `json:"user_name"`
	ChannelId   string                 `json:"channel_id"`
	ChannelName string                 `json:"channel_name"`
	TeamId      string                 `json:"team_id"`
	PostId      string                 `json:"post_id"`
	TriggerId   string                 `json:"trigger_id"`
	Type        string                 `json:"type"`
	DataSource  string                 `json:"data_source"`
	Context     map[string]interface{} `json:"context"`
	Action      struct {
		Id          string                 `json:"id"`
		Name        string                 `json:"name"`
		Integration map[string]interface{} `json:"integration"`
	} `json:"action"`
}

func (p *BotButtonWebhookPlugin) handleButtonClick(w http.ResponseWriter, r *http.Request) {

	p.API.LogInfo("meda-plugin: === handleButtonClick called ===",
		"path", r.URL.Path,
		"method", r.Method,
		"vars", mux.Vars(r))

	// Читаем тело запроса
	body, err := io.ReadAll(r.Body)
	if err != nil {
		p.API.LogError("meda-plugin: Failed to read request body", "err", err.Error())
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	p.API.LogInfo("meda-plugin: Received button click", "body", string(body))

	// Парсим interactive message payload
	var payload InteractiveMessagePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		p.API.LogError("meda-plugin: Invalid JSON in button click", "err", err.Error())
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Если PostId пустой, пытаемся найти пост по контексту
	if payload.PostId == "" && payload.Context != nil {
		if postId, ok := payload.Context["post_id"].(string); ok {
			payload.PostId = postId
		}
	}

	if payload.PostId == "" {
		p.API.LogError("meda-plugin: No post_id in payload or context")
		http.Error(w, "No post_id", http.StatusBadRequest)
		return
	}

	// Получаем пост
	post, appErr := p.API.GetPost(payload.PostId)
	if appErr != nil {
		p.API.LogError("meda-plugin: Failed to get post", "post_id", payload.PostId, "err", appErr.Error())
		http.Error(w, "Post not found", http.StatusNotFound)
		return
	}

	// Определяем бота
	botID := post.UserId
	webhookURL, exists := p.botWebhooks[botID]
	if !exists {
		p.API.LogWarn("meda-plugin: No webhook configured for bot", "bot_id", botID)

		// Вместо ошибки, отправляем ephemeral сообщение пользователю
		ephemeralPost := &model.Post{
			UserId:    botID,
			ChannelId: payload.ChannelId,
			Message:   "meda-plugin: No webhook configured for this bot",
		}
		p.API.SendEphemeralPost(payload.UserId, ephemeralPost)

		http.Error(w, "meda-plugin: No handler for this bot", http.StatusNotFound)
		return
	}

	// Получаем action_id из URL или из payload
	actionId := mux.Vars(r)["action_id"]
	if actionId == "" && payload.Action.Id != "" {
		actionId = payload.Action.Id
	}

	// Формируем данные для отправки
	dataToSend := map[string]interface{}{
		"user_id":      payload.UserId,
		"user_name":    payload.UserName,
		"channel_id":   payload.ChannelId,
		"channel_name": payload.ChannelName,
		"team_id":      payload.TeamId,
		"post_id":      payload.PostId,
		"action_id":    actionId,
		"context":      payload.Context,
		"trigger_id":   payload.TriggerId,
		"bot_id":       botID,
		"action":       payload.Action,
		"type":         payload.Type,
	}

	jsonData, _ := json.Marshal(dataToSend)
	p.API.LogInfo("meda-plugin: Sending to webhook", "url", webhookURL, "data", string(jsonData))

	// Отправляем POST на webhook
	resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		p.API.LogError("meda-plugin: Failed to send to webhook", "url", webhookURL, "err", err.Error())
		http.Error(w, "Webhook error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Обновляем оригинальный пост: убираем кнопки и показываем кто и что выбрал.
	// Это надёжнее ephemeral_text — визуально меняет пост для всех участников.
	response := map[string]interface{}{
		"update": map[string]interface{}{
			"props": map[string]interface{}{
				"attachments": []map[string]interface{}{
					{
						"text": "✅ Действие выполнено: *" + actionId + "* — пользователь @" + payload.UserName,
					},
				},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func main() {
	plugin.ClientMain(&BotButtonWebhookPlugin{})
}
