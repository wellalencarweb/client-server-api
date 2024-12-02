package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Configuração de timeouts
const (
	APITimeout   = 200 * time.Millisecond
	DBTimeout    = 10 * time.Millisecond
	ServerPort   = ":8080"
	QuotesAPIURL = "https://economia.awesomeapi.com.br/json/last/USD-BRL"
)

// Estrutura para representar a resposta da API
type Quote struct {
	Bid string `json:"bid"`
}

type APIResponse struct {
	USDBRL Quote `json:"USDBRL"`
}

// fetchDollarQuote busca a cotação do dólar usando um contexto com timeout
func fetchDollarQuote(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, QuotesAPIURL, nil)
	if err != nil {
		return "", fmt.Errorf("erro ao criar requisição: %w", err)
	}

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("erro ao fazer requisição: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status da API não OK: %d", resp.StatusCode)
	}

	var apiResponse APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		return "", fmt.Errorf("erro ao decodificar resposta: %w", err)
	}

	return apiResponse.USDBRL.Bid, nil
}

// saveQuoteToDatabase salva a cotação no banco de dados com um contexto de timeout
func saveQuoteToDatabase(ctx context.Context, db *sql.DB, bid string) error {
	query := `INSERT INTO quotes (bid, timestamp) VALUES (?, ?)`
	stmt, err := db.Prepare(query)
	if err != nil {
		return fmt.Errorf("erro ao preparar query: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.ExecContext(ctx, bid, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("erro ao executar query: %w", err)
	}

	return nil
}

// handleQuote processa a solicitação do cliente para obter a cotação
func handleQuote(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	ctx := r.Context()

	// Busca a cotação do dólar com timeout
	apiCtx, cancelAPICtx := context.WithTimeout(ctx, APITimeout)
	defer cancelAPICtx()

	bid, err := fetchDollarQuote(apiCtx)
	if err != nil {
		log.Printf("Erro ao buscar cotação: %v", err)
		http.Error(w, "Erro ao buscar cotação", http.StatusInternalServerError)
		return
	}

	// Salva a cotação no banco de dados com timeout
	dbCtx, cancelDBCtx := context.WithTimeout(ctx, DBTimeout)
	defer cancelDBCtx()

	if err := saveQuoteToDatabase(dbCtx, db, bid); err != nil {
		log.Printf("Erro ao salvar no banco: %v", err)
		http.Error(w, "Erro ao salvar no banco", http.StatusInternalServerError)
		return
	}

	// Retorna a cotação para o cliente
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"bid": bid})
}

func setupDatabase() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", "./quotes.db")
	if err != nil {
		return nil, fmt.Errorf("erro ao conectar ao banco: %w", err)
	}

	query := `CREATE TABLE IF NOT EXISTS quotes (
		id INTEGER PRIMARY KEY,
		bid TEXT,
		timestamp INTEGER
	)`
	if _, err := db.Exec(query); err != nil {
		return nil, fmt.Errorf("erro ao criar tabela: %w", err)
	}

	return db, nil
}

func main() {
	// Configuração do banco de dados
	db, err := setupDatabase()
	if err != nil {
		log.Fatalf("Erro na configuração do banco de dados: %v", err)
	}
	defer db.Close()

	// Configuração do servidor HTTP
	http.HandleFunc("/cotacao", func(w http.ResponseWriter, r *http.Request) {
		handleQuote(w, r, db)
	})

	log.Printf("Servidor iniciado na porta %s...", ServerPort)
	log.Fatal(http.ListenAndServe(ServerPort, nil))
}
