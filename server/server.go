package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Config armazena configurações do servidor
type Config struct {
	ServerAddress string
	QuotesAPIURL  string
	FetchTimeout  time.Duration
	InsertTimeout time.Duration
}

// Quote representa o modelo da tabela de cotações
type Quote struct {
	ID        uint      `gorm:"primaryKey"`
	Bid       string    `gorm:"not null"`
	CreatedAt time.Time `gorm:"autoCreateTime;not null"`
}

const (
	ErrFetchingQuote    = "Erro ao buscar cotação"
	ErrDecodingResponse = "Erro ao decodificar resposta"
	ErrDatabaseInsert   = "Erro ao inserir no banco de dados"
)

// main inicializa o servidor, carregando as configurações e configurando o banco de dados.
// Ele define o endpoint HTTP para tratar requisições de cotação e inicia o servidor.
func main() {
	log.Println("Iniciando servidor...")

	config, err := loadConfig()
	if err != nil {
		log.Fatalf("Erro ao carregar configurações: %v", err)
		return
	}

	db, err := setupDatabase()
	if err != nil {
		log.Fatalf("Erro ao configurar banco de dados: %v", err)
		return
	}

	http.HandleFunc("/cotacao", handleQuote(config, db))
	log.Printf("Servidor ouvindo em %s", config.ServerAddress)

	if err := http.ListenAndServe(config.ServerAddress, nil); err != nil {
		log.Fatalf("Erro ao iniciar servidor: %v", err)
	}
}

// loadConfig carrega as configurações do ambiente ou valores padrão
func loadConfig() (*Config, error) {
	fetchTimeoutStr := getEnv("FETCH_TIMEOUT", "200ms")
	fetchTimeout, err := time.ParseDuration(fetchTimeoutStr)
	if err != nil {
		return nil, fmt.Errorf("erro ao parsear FETCH_TIMEOUT (%s): %w", fetchTimeoutStr, err)
	}

	insertTimeoutStr := getEnv("INSERT_TIMEOUT", "10ms")
	insertTimeout, err := time.ParseDuration(insertTimeoutStr)
	if err != nil {
		return nil, fmt.Errorf("erro ao parsear INSERT_TIMEOUT (%s): %w", insertTimeoutStr, err)
	}

	return &Config{
		ServerAddress: getEnv("SERVER_ADDRESS", ":8080"),
		QuotesAPIURL:  getEnv("QUOTES_API_URL", "https://economia.awesomeapi.com.br/json/last/USD-BRL"),
		FetchTimeout:  fetchTimeout,
		InsertTimeout: insertTimeout,
	}, nil
}

// getEnv retorna o valor de uma variável de ambiente ou um fallback
func getEnv(key, fallback string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		return fallback
	}
	return value
}

// setupDatabase configura o banco de dados e aplica as migrações
func setupDatabase() (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open("quotes.db"), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("erro ao conectar ao banco de dados: %w", err)
	}

	if db == nil {
		return nil, errors.New("banco de dados não inicializado corretamente")
	}

	if err := db.AutoMigrate(&Quote{}); err != nil {
		return nil, fmt.Errorf("erro ao realizar migração no banco de dados: %w", err)
	}

	return db, nil
}

// handleQuote processa requisições para o endpoint /cotacao
func handleQuote(config *Config, db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Println("Recebendo requisição para /cotacao")

		if config == nil {
			http.Error(w, "Configuração do servidor ausente", http.StatusInternalServerError)
			return
		}
		if db == nil {
			http.Error(w, "Banco de dados não inicializado", http.StatusInternalServerError)
			return
		}

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
			http.Error(w, "Erro ao buscar cotação", http.StatusInternalServerError)
			return
		}

		ctx, cancel = context.WithTimeout(context.Background(), config.InsertTimeout)
		defer cancel()

		if err := saveQuote(ctx, db, bid); err != nil {
			log.Printf("%s: %v", ErrDatabaseInsert, err)
			http.Error(w, "Erro ao salvar cotação no banco", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"bid": bid}); err != nil {
			log.Printf("Erro ao codificar resposta JSON: %v", err)
			http.Error(w, "Erro interno do servidor", http.StatusInternalServerError)
		}
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
func saveQuote(ctx context.Context, db *gorm.DB, bid string) error {
	if db == nil {
		return errors.New("banco de dados nulo")
	}

	quote := Quote{Bid: bid}
	if err := db.WithContext(ctx).Create(&quote).Error; err != nil {
		return err
	}
	return nil
}
