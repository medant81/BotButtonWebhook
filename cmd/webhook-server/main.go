package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

func webhookHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "ошибка чтения тела запроса", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var pretty map[string]interface{}
	if err := json.Unmarshal(body, &pretty); err != nil {
		log.Printf("=== ВХОДЯЩИЙ ЗАПРОС (сырые данные) ===\n%s\n", string(body))
	} else {
		formatted, _ := json.MarshalIndent(pretty, "", "  ")
		log.Printf("=== ВХОДЯЩИЙ ЗАПРОС ===\n%s\n", string(formatted))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, `{"status":"ok"}`)
}

func main() {
	http.HandleFunc("/webhook", webhookHandler)
	log.Println("Сервер запущен на :9090  →  POST /webhook")
	log.Fatal(http.ListenAndServe(":9090", nil))
}
