package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Configurações e constantes
const (
	QuotesAPIURL          = "https://economia.awesomeapi.com.br/json/last/USD-BRL"
	ServerPort            = ":8080"
	QuotesFetchTimeout    = 200 * time.Millisecond
	DatabaseInsertTimeout = 10 * time.Millisecond
	DatabaseFile          = "quotes.db"
)

// Logger centralizado
func logError(err error, msg string) {
	log.Printf("[ERRO] %s: %v", msg, err)
}

func logInfo(msg string) {
	log.Printf("[INFO] %s", msg)
}

// Fetcher interface para abstrair requisições HTTP
type Fetcher interface {
	Fetch(ctx context.Context, url string) ([]byte, error)
}

type HTTPFetcher struct{}

// Fetch faz a requisição HTTP
func (f *HTTPFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar requisição: %w", err)
	}

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("erro ao realizar requisição: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status não OK: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("erro ao ler resposta: %w", err)
	}

	return body, nil
}

// saveQuoteToDatabase insere a cotação no banco de dados
func saveQuoteToDatabase(ctx context.Context, db *sql.DB, bid string) error {
	query := "INSERT INTO quotes (bid, created_at) VALUES (?, ?)"
	stmt, err := db.Prepare(query)
	if err != nil {
		return fmt.Errorf("erro ao preparar query: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.ExecContext(ctx, bid, time.Now())
	if err != nil {
		return fmt.Errorf("erro ao executar query: %w", err)
	}

	return nil
}

// handleQuote lida com requisições HTTP para o endpoint /cotacao
func handleQuote(fetcher Fetcher, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), QuotesFetchTimeout)
		defer cancel()

		body, err := fetcher.Fetch(ctx, QuotesAPIURL)
		if err != nil {
			logError(err, "Falha ao buscar cotação")
			http.Error(w, "Erro ao buscar cotação", http.StatusInternalServerError)
			return
		}

		var data map[string]map[string]string
		if err := json.Unmarshal(body, &data); err != nil {
			logError(err, "Falha ao decodificar resposta")
			http.Error(w, "Erro no servidor", http.StatusInternalServerError)
			return
		}

		bid, ok := data["USDBRL"]["bid"]
		if !ok {
			err := fmt.Errorf("campo 'bid' não encontrado na resposta")
			logError(err, "Falha na validação de dados")
			http.Error(w, "Erro ao processar cotação", http.StatusInternalServerError)
			return
		}

		dbCtx, dbCancel := context.WithTimeout(ctx, DatabaseInsertTimeout)
		defer dbCancel()

		if err := saveQuoteToDatabase(dbCtx, db, bid); err != nil {
			logError(err, "Falha ao salvar cotação no banco")
			http.Error(w, "Erro ao salvar cotação", http.StatusInternalServerError)
			return
		}

		response := map[string]string{"bid": bid}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

func setupDatabase() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", DatabaseFile)
	if err != nil {
		return nil, fmt.Errorf("erro ao abrir banco de dados: %w", err)
	}

	query := `
	CREATE TABLE IF NOT EXISTS quotes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		bid TEXT NOT NULL,
		created_at DATETIME NOT NULL
	)`
	if _, err := db.Exec(query); err != nil {
		return nil, fmt.Errorf("erro ao criar tabela: %w", err)
	}

	return db, nil
}

func main() {
	logInfo("Iniciando servidor...")

	db, err := setupDatabase()
	if err != nil {
		log.Fatalf("[ERRO] Falha ao configurar banco de dados: %v", err)
	}
	defer db.Close()

	fetcher := &HTTPFetcher{}
	http.HandleFunc("/cotacao", handleQuote(fetcher, db))

	logInfo("Servidor ouvindo na porta 8080")
	log.Fatal(http.ListenAndServe(ServerPort, nil))
}
