package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Quote struct {
	Bid string `json:"bid"`
}

type APIResponse struct {
	USDBRL Quote `json:"USDBRL"`
}

func fetchDollarQuote(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://economia.awesomeapi.com.br/json/last/USD-BRL", nil)
	if err != nil {
		return "", err
	}

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var apiResp APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return "", err
	}

	return apiResp.USDBRL.Bid, nil
}

func saveToDatabase(ctx context.Context, db *sql.DB, bid string) error {
	query := `INSERT INTO quotes (bid, timestamp) VALUES (?, ?)`
	stmt, err := db.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.ExecContext(ctx, bid, time.Now().Unix())
	return err
}

func handleQuote(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	ctx := r.Context()

	// Timeout para a API externa
	apiCtx, cancelAPI := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancelAPI()

	bid, err := fetchDollarQuote(apiCtx)
	if err != nil {
		log.Printf("Erro ao buscar cotação: %v", err)
		http.Error(w, "Erro ao buscar cotação", http.StatusInternalServerError)
		return
	}

	// Timeout para o banco de dados
	dbCtx, cancelDB := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancelDB()

	if err := saveToDatabase(dbCtx, db, bid); err != nil {
		log.Printf("Erro ao salvar no banco: %v", err)
		http.Error(w, "Erro ao salvar no banco", http.StatusInternalServerError)
		return
	}

	// Retorna a cotação para o cliente
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"bid": bid})
}

func main() {
	// Configuração do banco de dados SQLite
	db, err := sql.Open("sqlite3", "./quotes.db")
	if err != nil {
		log.Fatalf("Erro ao conectar no banco de dados: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS quotes (id INTEGER PRIMARY KEY, bid TEXT, timestamp INTEGER)`)
	if err != nil {
		log.Fatalf("Erro ao criar tabela no banco de dados: %v", err)
	}

	http.HandleFunc("/cotacao", func(w http.ResponseWriter, r *http.Request) {
		handleQuote(w, r, db)
	})

	log.Println("Servidor iniciado na porta 8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
