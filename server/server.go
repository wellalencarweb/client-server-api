package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Config armazena configurações do servidor
type Config struct {
	ServerAddress string
	QuotesAPIURL  string
	FetchTimeout  time.Duration
	InsertTimeout time.Duration
	DatabaseFile  string
}

const (
	ErrFetchingQuote    = "erro ao buscar cotação"
	ErrDecodingResponse = "erro ao decodificar resposta"
	ErrDatabaseInsert   = "erro ao inserir no banco de dados"
)

func main() {
	log.Println("Iniciando servidor...")

	config, err := loadConfig()
	if err != nil {
		log.Fatalf("erro ao carregar configurações: %v", err)
	}

	db, err := setupDatabase(config.DatabaseFile)
	if err != nil {
		log.Fatalf("erro ao configurar banco de dados: %v", err)
	}
	defer db.Close()

	http.HandleFunc("/cotacao", handleQuote(config, db))
	log.Printf("Servidor ouvindo em %s", config.ServerAddress)
	log.Fatal(http.ListenAndServe(config.ServerAddress, nil))
}

// loadConfig carrega as configurações do ambiente ou valores padrão
func loadConfig() (*Config, error) {
	fetchTimeout, err := time.ParseDuration(getEnv("FETCH_TIMEOUT", "200ms"))
	if err != nil {
		return nil, fmt.Errorf("erro ao parsear FETCH_TIMEOUT: %w", err)
	}

	insertTimeout, err := time.ParseDuration(getEnv("INSERT_TIMEOUT", "10ms"))
	if err != nil {
		return nil, fmt.Errorf("erro ao parsear INSERT_TIMEOUT: %w", err)
	}

	return &Config{
		ServerAddress: getEnv("SERVER_ADDRESS", ":8080"),
		QuotesAPIURL:  getEnv("QUOTES_API_URL", "https://economia.awesomeapi.com.br/json/last/USD-BRL"),
		FetchTimeout:  fetchTimeout,
		InsertTimeout: insertTimeout,
		DatabaseFile:  getEnv("DATABASE_FILE", "quotes.db"),
	}, nil
}

// getEnv retorna o valor de uma variável de ambiente ou um fallback
func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

// setupDatabase configura a conexão com o banco de dados
func setupDatabase(databaseFile string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", databaseFile)
	if err != nil {
		return nil, fmt.Errorf("erro ao conectar ao banco de dados: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("erro ao verificar conexão com o banco de dados: %w", err)
	}

	// Cria a tabela 'quotes' se não existir
	createTableQuery := `
	CREATE TABLE IF NOT EXISTS quotes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		bid TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := db.Exec(createTableQuery); err != nil {
		return nil, fmt.Errorf("erro ao criar tabela no banco de dados: %w", err)
	}

	return db, nil
}

// handleQuote processa requisições para o endpoint /cotacao
func handleQuote(config *Config, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Println("Recebendo requisição para /cotacao")

		ctx, cancel := context.WithTimeout(r.Context(), config.FetchTimeout)
		defer cancel()

		bid, err := fetchQuote(ctx, config.QuotesAPIURL)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				log.Printf("%s: %v", ErrFetchingQuote, err)
				http.Error(w, "Timeout na requisição à API", http.StatusGatewayTimeout)
				return
			}
			log.Printf("%s: %v", ErrFetchingQuote, err)
			http.Error(w, "erro ao buscar cotação", http.StatusInternalServerError)
			return
		}

		ctx, cancel = context.WithTimeout(context.Background(), config.InsertTimeout)
		defer cancel()

		if err := saveQuote(ctx, db, bid); err != nil {
			log.Printf("%s: %v", ErrDatabaseInsert, err)
			http.Error(w, "erro ao salvar cotação no banco", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"bid": bid})
	}
}

// fetchQuote busca a cotação na API externa
func fetchQuote(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("erro ao criar requisição: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("erro ao executar requisição: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resposta inesperada da API: %d", resp.StatusCode)
	}

	var data map[string]map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("%s: %w", ErrDecodingResponse, err)
	}

	bid, ok := data["USDBRL"]["bid"]
	if !ok || bid == "" {
		return "", errors.New("campo 'bid' ausente ou inválido na resposta")
	}
	return bid, nil
}

// saveQuote insere a cotação no banco de dados
func saveQuote(ctx context.Context, db *sql.DB, bid string) error {
	query := "INSERT INTO quotes (bid, created_at) VALUES (?, datetime('now'))"
	_, err := db.ExecContext(ctx, query, bid)
	return err
}
