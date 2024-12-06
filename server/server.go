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
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

const (
	ErrFetchingQuote    = "Erro ao buscar cotação"
	ErrDecodingResponse = "Erro ao decodificar resposta"
	ErrDatabaseInsert   = "Erro ao inserir no banco de dados"
)

func main() {
	log.Println("Iniciando servidor...")

	config, err := loadConfig()
	if err != nil {
		log.Fatalf("Erro ao carregar configurações: %v", err)
	}

	db, err := setupDatabase()
	if err != nil {
		log.Fatalf("Erro ao configurar banco de dados: %v", err)
	}

	http.HandleFunc("/cotacao", handleQuote(config, db))
	log.Printf("Servidor ouvindo em %s", config.ServerAddress)
	log.Fatal(http.ListenAndServe(config.ServerAddress, nil))
}

// loadConfig carrega as configurações do ambiente ou valores padrão
func loadConfig() (*Config, error) {
	fetchTimeout, err := time.ParseDuration(getEnv("FETCH_TIMEOUT", "200ms"))
	if err != nil {
		return nil, fmt.Errorf("Erro ao parsear FETCH_TIMEOUT: %w", err)
	}

	insertTimeout, err := time.ParseDuration(getEnv("INSERT_TIMEOUT", "10ms"))
	if err != nil {
		return nil, fmt.Errorf("Erro ao parsear INSERT_TIMEOUT: %w", err)
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
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

// setupDatabase configura o banco de dados e aplica as migrações
func setupDatabase() (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open("quotes.db"), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("Erro ao conectar ao banco de dados: %w", err)
	}

	// AutoMigrate para criar ou atualizar a tabela de cotações
	if err := db.AutoMigrate(&Quote{}); err != nil {
		return nil, fmt.Errorf("Erro ao realizar migração no banco de dados: %w", err)
	}

	return db, nil
}

// handleQuote processa requisições para o endpoint /cotacao
func handleQuote(config *Config, db *gorm.DB) http.HandlerFunc {
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
		json.NewEncoder(w).Encode(map[string]string{"bid": bid})
	}
}

// fetchQuote busca a cotação na API externa
func fetchQuote(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("Erro ao criar requisição: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("Erro ao executar requisição: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Resposta inesperada da API: %d", resp.StatusCode)
	}

	var data map[string]map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("%s: %w", ErrDecodingResponse, err)
	}

	bid, ok := data["USDBRL"]["bid"]
	if !ok || bid == "" {
		return "", errors.New("Campo 'bid' ausente ou inválido na resposta")
	}
	return bid, nil
}

// saveQuote insere a cotação no banco de dados
func saveQuote(ctx context.Context, db *gorm.DB, bid string) error {
	quote := Quote{Bid: bid}
	if err := db.WithContext(ctx).Create(&quote).Error; err != nil {
		return err
	}
	return nil
}
