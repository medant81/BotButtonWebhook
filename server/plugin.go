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
	// Регистрируем обработчик для всех action-запросов
	p.router = mux.NewRouter()
	p.router.HandleFunc("/actions/{action_id:[a-zA-Z0-9_-]+}", p.handleButtonClick).Methods("POST")

	p.API.RegisterHTTPHandler("/", p)

	p.loadConfiguration() // Загружаем webhook'и при активации
	p.API.LogInfo("Button Handler plugin activated with custom routes")
	return nil
}

// ServeHTTP — обязательный метод, который Mattermost вызывает для всех запросов к плагину
func (p *BotButtonWebhookPlugin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.router.ServeHTTP(w, r)
}

// Загрузка конфигурации (список ботов и их webhook)
func (p *BotButtonWebhookPlugin) loadConfiguration() {
	config := p.getConfiguration()
	p.botWebhooks = make(map[string]string)

	// Пример строки из настроек: "bot_id1: https://url1.com, bot_id2: https://url2.com"
	pairs := strings.Split(config.BotWebhooks, ",")
	for _, pair := range pairs {
		parts := strings.SplitN(strings.TrimSpace(pair), ":", 2)
		if len(parts) == 2 {
			botID := strings.TrimSpace(parts[0])
			url := strings.TrimSpace(parts[1])
			p.botWebhooks[botID] = url
			p.API.LogInfo("Added webhook for bot", "bot_id", botID, "url", url)
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
		p.API.LogError("Failed to load config", "err", err.Error())
		return &Configuration{}
	}
	return &config
}

// Главная функция — обработчик нажатия на кнопку
func (p *BotButtonWebhookPlugin) handleButtonClick(w http.ResponseWriter, r *http.Request) {
	// 1. Читаем тело запроса от Mattermost
	body, err := io.ReadAll(r.Body)
	if err != nil {
		p.API.LogError("Failed to read request body", "err", err.Error())
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// 2. Парсим данные о клике (Mattermost отправляет JSON)
	var payload struct {
		UserID    string          `json:"user_id"`
		ChannelID string          `json:"channel_id"`
		PostID    string          `json:"post_id"`
		Context   json.RawMessage `json:"context"`
		TriggerID string          `json:"trigger_id,omitempty"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		p.API.LogError("Invalid JSON in button click", "err", err.Error())
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// 3. Получаем ID поста и самого поста, чтобы узнать, от какого бота он
	post, appErr := p.API.GetPost(payload.PostID)
	if appErr != nil {
		p.API.LogError("Failed to get post for button click", "err", appErr.Error())
		http.Error(w, "Post not found", http.StatusNotFound)
		return
	}

	// 4. Определяем, от какого бота сообщение (UserId поста = ID бота)
	botID := post.UserId
	webhookURL, exists := p.botWebhooks[botID]
	if !exists {
		p.API.LogWarn("No webhook configured for bot", "bot_id", botID)
		http.Error(w, "No handler for this bot", http.StatusNotFound)
		return
	}

	// 5. Формируем данные для отправки (можно добавить/убрать поля)
	dataToSend := map[string]interface{}{
		"user_id":    payload.UserID,
		"channel_id": payload.ChannelID,
		"post_id":    payload.PostID,
		"action_id":  mux.Vars(r)["action_id"],
		"context":    payload.Context,
		"trigger_id": payload.TriggerID,
		"bot_id":     botID,
	}

	jsonData, _ := json.Marshal(dataToSend)

	// 6. Отправляем POST на webhook бота
	resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		p.API.LogError("Failed to send to webhook", "url", webhookURL, "err", err.Error())
		http.Error(w, "Webhook error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 7. Обязательно возвращаем 200 OK клиенту Mattermost
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func main() {
	plugin.ClientMain(&BotButtonWebhookPlugin{})
}
