package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

const (
	serverURL          = "http://localhost:8080/cotacao"
	timeoutDuration    = 300 * time.Millisecond
	logFile            = "cotacao_log.txt"
	outputFile         = "cotacao.txt"
	ErrInvalidResponse = "Resposta inválida do servidor"
)

func main() {
	// Criar um contexto com timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration)
	defer cancel()

	// Fazer a requisição ao servidor
	bid, err := fetchDollarQuote(ctx, serverURL)
	if err != nil {
		logError(err)
		fmt.Println("Erro ao buscar cotação. Detalhes salvos no log.")
		return
	}

	// Salvar a cotação no arquivo
	if err := saveQuoteToFile(outputFile, fmt.Sprintf("Dólar: %s", bid)); err != nil {
		logError(fmt.Errorf("erro ao salvar cotação no arquivo: %w", err))
		fmt.Println("Erro ao salvar cotação.")
		return
	}

	fmt.Println("Cotação salva com sucesso em", outputFile)
}

// fetchDollarQuote faz uma requisição HTTP ao servidor para obter a cotação do dólar
func fetchDollarQuote(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("erro ao criar requisição: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", fmt.Errorf("timeout na requisição ao servidor: %w", err)
		}
		return "", fmt.Errorf("erro ao realizar requisição ao servidor: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("%s. Status: %d, Mensagem: %s", ErrInvalidResponse, resp.StatusCode, string(body))
	}

	var data map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("erro ao decodificar resposta do servidor: %w", err)
	}

	bid, ok := data["bid"]
	if !ok || bid == "" {
		return "", errors.New("campo 'bid' ausente ou inválido na resposta do servidor")
	}

	return bid, nil
}

// saveQuoteToFile salva a cotação em um arquivo
func saveQuoteToFile(filename, content string) error {
	if filename == "" {
		return errors.New("nome do arquivo não pode ser nulo")
	}
	if content == "" {
		return errors.New("conteúdo da cotação não pode ser nulo")
	}
	return os.WriteFile(filename, []byte(content), 0644)
}

// logError salva mensagens de erro em um arquivo de log
func logError(err error) {
	logMessage := fmt.Sprintf("%s: %v\n", time.Now().Format(time.RFC3339), err)
	if logErr := os.WriteFile(logFile, []byte(logMessage), os.ModeAppend|0644); logErr != nil {
		log.Printf("Erro ao salvar no log: %v", logErr)
	}
	log.Println(err)
}
