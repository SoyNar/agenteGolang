package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

type FileResult struct {
	Path     string
	Code     string
	Phpstan  string
	HasIssue bool
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type GroqRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type GroqResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func GetGroqKey() string {
	return os.Getenv("GROQ_API_KEY")
}

func GetPhpstanConfig() string {
	config := os.Getenv("PHPSTAN_CONFIG")
	if config == "" {
		return "phpstan.neon"
	}
	return config
}

func RunPHPStan(file string) string {
	cmd := exec.Command("phpstan", "analyse",
		"--configuration", GetPhpstanConfig(),
		"--no-progress",
		file,
	)
	out, _ := cmd.CombinedOutput()
	return string(out)
}

func getLLM() (llms.Model, error) {
	return openai.New(
		openai.WithModel("llama-3.1-8b-instant"),
		openai.WithBaseURL("https://api.groq.com/openai/v1"),
		openai.WithToken(GetGroqKey()),
	)
}

func CallOllama(prompt string, onChunk func(string)) (string, error) {
	body := GroqRequest{
		Model: "llama-3.1-8b-instant",
		Messages: []Message{
			{Role: "user", Content: prompt},
		},
	}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "https://api.groq.com/openai/v1/chat/completions", bytes.NewBuffer(jsonBody))
	req.Header.Set("Authorization", "Bearer "+GetGroqKey())
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error llamando a Groq: %v", err)
	}
	defer resp.Body.Close()

	var result GroqResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("respuesta vacía de Groq")
	}

	text := result.Choices[0].Message.Content
	if onChunk != nil {
		onChunk(text)
	}

	return text, nil
}

func AnalyzeFile(file string) (FileResult, error) {
	if !strings.HasSuffix(file, ".php") {
		return FileResult{}, fmt.Errorf("solo se aceptan archivos .php")
	}
	info, err := os.Stat(file)
	if err != nil {
		return FileResult{}, fmt.Errorf("archivo no encontrado")
	}
	if info.IsDir() {
		return FileResult{}, fmt.Errorf("se esperaba un archivo, no una carpeta")
	}

	code, err := os.ReadFile(file)
	if err != nil {
		return FileResult{}, fmt.Errorf("error leyendo archivo: %v", err)
	}

	lines := strings.Split(string(code), "\n")
	if len(lines) > 300 {
		return FileResult{}, fmt.Errorf("archivo muy grande, máximo 300 líneas (tiene %d)", len(lines))
	}

	phpstan := RunPHPStan(file)
	output := strings.ToLower(phpstan)
	hasIssue := strings.Contains(output, "error") || strings.Contains(output, "warning")

	return FileResult{
		Path:     file,
		Code:     string(code),
		Phpstan:  phpstan,
		HasIssue: hasIssue,
	}, nil
}

func BuildPrompt(r FileResult) string {
	lines := strings.Split(r.Phpstan, "\n")
	var filtered []string
	for _, line := range lines {
		if !strings.Contains(line, "class.notFound") &&
			!strings.Contains(line, "not found") {
			filtered = append(filtered, line)
		}
	}
	phpstanClean := strings.Join(filtered, "\n")

	return fmt.Sprintf(`IMPORTANTE: Responde ÚNICAMENTE en español. No uses inglés bajo ninguna circunstancia.

Eres un senior PHP developer experto en migraciones PHP puro 7.4 → 8.1, sin frameworks.
IGNORA errores de clases no encontradas, son falsos positivos.
ANALIZA solo problemas reales de compatibilidad PHP 8.1.
Formato: línea X — problema — cómo corregirlo.

PHPSTAN:
%s`, phpstanClean)
}

// MigrateFile usa LangChain para migrar y verificar el código hasta 3 intentos
func MigrateFile(r FileResult) (string, error) {
	llm, err := getLLM()
	if err != nil {
		return "", fmt.Errorf("error iniciando LLM: %v", err)
	}

	ctx := context.Background()
	code := r.Code
	maxIntentos := 3

	for intento := 1; intento <= maxIntentos; intento++ {
		fmt.Printf("Intento %d de %d...\n", intento, maxIntentos)

		prompt := fmt.Sprintf(`Eres un senior PHP developer experto en migraciones PHP puro 7.4 → 8.1.
REGLAS ESTRICTAS:
1. Devuelve SOLO el código PHP, nada más.
2. PROHIBIDO agregar comentarios o explicaciones fuera del código.
3. PROHIBIDO usar bloques markdown.
4. El código debe empezar con <?php directamente.

Migra el siguiente código PHP a PHP 8.1:

%s`, code)

		// Llamar al LLM via LangChain
		fixedCode, err := llms.GenerateFromSinglePrompt(ctx, llm, prompt)
		if err != nil {
			return "", fmt.Errorf("error en LLM: %v", err)
		}

		// Limpiar markdown
		fixedCode = strings.TrimPrefix(fixedCode, "```php")
		fixedCode = strings.TrimPrefix(fixedCode, "```")
		fixedCode = strings.TrimSuffix(fixedCode, "```")
		fixedCode = strings.TrimSpace(fixedCode)

		if !strings.HasPrefix(fixedCode, "<?php") {
			continue
		}

		// Guardar temporal para verificar con PHPStan
		tmpFile, err := os.CreateTemp("", "*.php")
		if err != nil {
			return "", fmt.Errorf("error creando archivo temporal: %v", err)
		}
		tmpFile.WriteString(fixedCode)
		tmpFile.Close()

		// Verificar con PHPStan
		phpstanResult := RunPHPStan(tmpFile.Name())
		os.Remove(tmpFile.Name())

		output := strings.ToLower(phpstanResult)
		stillHasIssues := strings.Contains(output, "error") || strings.Contains(output, "warning")

		if !stillHasIssues {
			fmt.Printf("✅ Verificación exitosa en intento %d\n", intento)
			return fixedCode, nil
		}

		fmt.Printf("  Aún hay problemas, reintentando...\n")
		// Pasar el código corregido como base para el siguiente intento
		code = fixedCode
	}

	return code, nil
}
