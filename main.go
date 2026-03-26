package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const ollamaAPI = "http://localhost:11434/api/chat"

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Request struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type StreamResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

// FileResult guarda el resultado del escaneo de cada archivo
type FileResult struct {
	Path     string
	Code     string
	Phpstan  string
	HasIssue bool
}

// --- PHPStan ---

func runPHPStan(file string) string {
	cmd := exec.Command("phpstan", "analyse", "--level=5", "--no-progress", file)
	out, _ := cmd.CombinedOutput()
	return string(out)
}

// --- Ollama ---

func callOllama(prompt string) (string, error) {
	body := Request{
		Model:  "llama3.2",
		Stream: true,
		Messages: []Message{
			{Role: "user", Content: prompt},
		},
	}
	jsonBody, _ := json.Marshal(body)

	resp, err := http.Post(ollamaAPI, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("¿está corriendo Ollama? %v", err)
	}
	defer resp.Body.Close()

	var result strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var event StreamResponse
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		fmt.Print(event.Message.Content)
		result.WriteString(event.Message.Content)
		if event.Done {
			break
		}
	}
	fmt.Println()
	return result.String(), scanner.Err()
}

// --- Paso 1: escanear todos los archivos (rápido, sin LLM) ---

func scanFolder(folder string) ([]FileResult, error) {
	var results []FileResult

	err := filepath.WalkDir(folder, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "output_81" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".php") {
			return nil
		}

		code, err := os.ReadFile(path)
		if err != nil {
			fmt.Printf("  ⚠️  No se pudo leer %s\n", path)
			return nil
		}

		phpstan := runPHPStan(path)
		hasIssue := strings.Contains(phpstan, "error") || strings.Contains(phpstan, "Error")

		status := "✅"
		if hasIssue {
			status = "⚠️ "
		}
		fmt.Printf("  %s %s\n", status, path)

		results = append(results, FileResult{
			Path:     path,
			Code:     string(code),
			Phpstan:  phpstan,
			HasIssue: hasIssue,
		})
		return nil
	})

	return results, err
}

// --- Paso 2: reporte global al final ---

func printReport(results []FileResult) {
	// Construir un prompt con todos los archivos con problemas
	var sb strings.Builder
	sb.WriteString("Eres un senior PHP developer experto en migraciones PHP 7.4 → 8.1.\n")
	sb.WriteString("Analiza los siguientes archivos y sus resultados de PHPStan.\n")
	sb.WriteString("Para cada archivo lista los problemas de compatibilidad con PHP 8.1 en español.\n")
	sb.WriteString("Sé concreto: archivo, línea, problema, por qué falla en 8.1.\n\n")

	count := 0
	for _, r := range results {
		if !r.HasIssue {
			continue
		}
		count++
		sb.WriteString(fmt.Sprintf("=== %s ===\n", r.Path))
		sb.WriteString(fmt.Sprintf("PHPSTAN:\n%s\n\n", r.Phpstan))
	}

	if count == 0 {
		fmt.Println("🎉 No se encontraron problemas de compatibilidad.")
		return
	}

	fmt.Printf("\n📋 Analizando %d archivo(s) con problemas...\n\n", count)
	callOllama(sb.String())
}

// --- Paso 3: generar archivos corregidos ---

func generateFixes(results []FileResult, outputDir string) {
	for _, r := range results {
		fmt.Printf("\n🔧 Corrigiendo %s...\n", r.Path)

		prompt := fmt.Sprintf(`Reescribe este código PHP 7.4 para que sea compatible con PHP 8.1.
Devuelve ÚNICAMENTE el código PHP corregido, sin explicaciones ni bloques markdown.

--- CÓDIGO ---
%s`, r.Code)

		fixedCode, err := callOllama(prompt)
		if err != nil {
			fmt.Printf("  ⚠️  Error: %v\n", err)
			continue
		}

		// limpiar markdown si el modelo lo agrega
		fixedCode = strings.TrimPrefix(fixedCode, "```php")
		fixedCode = strings.TrimPrefix(fixedCode, "```")
		fixedCode = strings.TrimSuffix(fixedCode, "```")
		fixedCode = strings.TrimSpace(fixedCode)

		// respetar estructura de carpetas en output
		rel, _ := filepath.Rel(filepath.Dir(outputDir), r.Path)
		outFile := filepath.Join(outputDir, rel)
		os.MkdirAll(filepath.Dir(outFile), 0755)

		if err := os.WriteFile(outFile, []byte(fixedCode), 0644); err != nil {
			fmt.Printf("  ⚠️  No se pudo guardar: %v\n", err)
			continue
		}
		fmt.Printf("  ✅ Guardado en %s\n", outFile)
	}
}

// --- Main ---

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Uso: go run main.go <carpeta>")
		os.Exit(1)
	}

	folder := os.Args[1]
	outputDir := filepath.Join(folder, "output_81")

	fmt.Printf("\n🚀 Agente de migración PHP 7.4 → 8.1\n")
	fmt.Printf("📁 Carpeta: %s\n\n", folder)

	// Paso 1: escanear (rápido)
	fmt.Println("⚙️  Escaneando archivos con PHPStan...")
	results, err := scanFolder(folder)
	if err != nil {
		fmt.Printf("Error escaneando carpeta: %v\n", err)
		os.Exit(1)
	}
	if len(results) == 0 {
		fmt.Println("No se encontraron archivos .php.")
		os.Exit(0)
	}

	withIssues := 0
	for _, r := range results {
		if r.HasIssue {
			withIssues++
		}
	}

	fmt.Printf("\n📦 %d archivo(s) encontrados — %d con posibles problemas\n", len(results), withIssues)
	fmt.Println(strings.Repeat("═", 50))

	// Preguntar modo
	fmt.Println("\n¿Qué quieres hacer?")
	fmt.Println("  [1] Solo ver el reporte de problemas")
	fmt.Println("  [2] Ver reporte + generar archivos corregidos")
	fmt.Print("\nOpción (1/2): ")

	reader := bufio.NewReader(os.Stdin)
	option, _ := reader.ReadString('\n')
	option = strings.TrimSpace(option)

	fmt.Println()

	// Paso 2: reporte siempre
	fmt.Println(strings.Repeat("─", 50))
	printReport(results)
	fmt.Println(strings.Repeat("─", 50))

	// Paso 3: corregir solo si eligió opción 2
	if option == "2" {
		fmt.Printf("\n💾 Generando archivos corregidos en %s\n", outputDir)
		fmt.Println(strings.Repeat("─", 50))
		generateFixes(results, outputDir)
		fmt.Printf("\n✅ Listo. Originales intactos en %s\n", folder)
		fmt.Printf("   Corregidos en %s\n", outputDir)
	} else {
		fmt.Println("\nReporte completo. Corre con opción 2 cuando quieras generar los archivos corregidos.")
	}
}
