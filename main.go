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
	cmd := exec.Command("phpstan", "analyse", "--configuration", "/home/nar/GolandProjects/my-agent/phpstan.neon", "--no-progress", file)
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

func scanFolder(file string) ([]FileResult, error) {
	// En vez de scanFolder, leer directamente el archivo
	code, err := os.ReadFile(file)
	if err != nil {
		fmt.Printf("Error leyendo archivo: %v\n", err)
		os.Exit(1)
	}

	phpstan := runPHPStan(file)
	output := strings.ToLower(phpstan)
	hasIssue := strings.Contains(output, "error") || strings.Contains(output, "warning")

	results := []FileResult{{
		Path:     file,
		Code:     string(code),
		Phpstan:  phpstan,
		HasIssue: hasIssue,
	}}
	return results, nil
}

// --- Paso 2: reporte global al final ---

func printReport(results []FileResult, reportPath string) {
	var reportLines strings.Builder

	hasAny := false
	for _, r := range results {
		if !r.HasIssue {
			continue
		}
		hasAny = true
	}
	if !hasAny {
		fmt.Println("🎉 No se encontraron problemas de compatibilidad.")
		return
	}

	for _, r := range results {
		if !r.HasIssue {
			continue
		}

		lines := strings.Split(r.Phpstan, "\n")
		var filtered []string
		for _, line := range lines {
			if !strings.Contains(line, "class.notFound") &&
				!strings.Contains(line, "not found") {
				filtered = append(filtered, line)
			}
		}
		phpstanClean := strings.Join(filtered, "\n")

		errorCount := strings.Count(r.Phpstan, "at /home")
		if errorCount > 100 {
			fmt.Printf(" %d errores detectados, el análisis puede tardar...\n", errorCount)
		}

		fmt.Printf("\nAnalizando %s...\n\n", r.Path)

		prompt := fmt.Sprintf(`Eres un senior PHP developer experto en migraciones PHP puro 7.4 → 8.1, sin frameworks.
IGNORA errores de clases no encontradas, son falsos positivos.
ANALIZA solo problemas reales de compatibilidad PHP 8.1.
Responde siempre en español.
Formato: línea X — problema — cómo corregirlo.

PHPSTAN:
%s`, phpstanClean)

		// CAMBIO: capturar respuesta de Ollama
		response, err := callOllama(prompt)
		if err == nil {
			// Agregar al reporte .md
			reportLines.WriteString(fmt.Sprintf("## %s\n\n", r.Path))
			reportLines.WriteString(response)
			reportLines.WriteString("\n\n---\n\n")
		}

		fmt.Println(strings.Repeat("─", 40))
	}

	// Guardar el .md
	if reportLines.Len() > 0 {
		content := "# Reporte de migración PHP 7.4 → 8.1\n\n" + reportLines.String()
		if err := os.WriteFile(reportPath, []byte(content), 0644); err != nil {
			fmt.Printf(" No se pudo guardar el reporte: %v\n", err)
		} else {
			fmt.Printf("\n Reporte guardado en %s\n", reportPath)
		}
	}
}
func generateFixes(results []FileResult, outputDir string) {
	for _, r := range results {
		fmt.Printf("\n Corrigiendo %s...\n", r.Path)

		prompt := fmt.Sprintf(`Reescribe este código PHP 7.4 para que sea compatible con PHP 8.1.
Devuelve ÚNICAMENTE el código PHP corregido, sin explicaciones ni bloques markdown.

--- CÓDIGO ---
%s`, r.Code)

		fixedCode, err := callOllama(prompt)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
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
			fmt.Printf("   No se pudo guardar: %v\n", err)
			continue
		}
		fmt.Printf("  Guardado en %s\n", outFile)
	}
}

// --- Main ---

func main() {

	if len(os.Args) < 2 {
		fmt.Println("Uso: go run main.go <archivo.php>")
		os.Exit(1)
	}

	file := os.Args[1]

	if !strings.HasSuffix(file, ".php") {
		fmt.Println("Solo se aceptan archivos .php")
		os.Exit(1)
	}

	info, err := os.Stat(file)
	if err != nil {
		fmt.Println("Archivo no encontrado")
		os.Exit(1)
	}
	if info.IsDir() {
		fmt.Println("Se esperaba un archivo, no una carpeta")
		os.Exit(1)
	}
	if info.Size() > 500*1024 {
		fmt.Println("Archivo muy grande, máximo 500KB")
		os.Exit(1)
	}

	outputDir := filepath.Join(filepath.Dir(file), "output_81")

	fmt.Printf("\nAgente de migración PHP 7.4 → 8.1\n")
	fmt.Printf(" archivo: %s\n\n", file)

	// Paso 1: escanear (rápido)
	fmt.Println("Escaneando archivo con PHPStan...")
	results, err := scanFolder(file)
	if err != nil {
		fmt.Printf("Error escaneando archivo: %v\n", err)
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

	fmt.Printf("\n %d archivo(s) encontrados — %d con posibles problemas\n", len(results), withIssues)
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
	reportPath := filepath.Join(filepath.Dir(file), "reporte_81.md")

	fmt.Println(strings.Repeat("─", 50))
	printReport(results, reportPath) // ← agregar reportPath
	fmt.Println(strings.Repeat("─", 50))

	// Paso 3: corregir solo si eligió opción 2
	if option == "2" {
		fmt.Printf("\n Generando archivos corregidos en %s\n", outputDir)
		fmt.Println(strings.Repeat("─", 50))
		generateFixes(results, outputDir)
		fmt.Printf("\n Listo. Originales intactos en %s\n", file)
		fmt.Printf("   Corregidos en %s\n", outputDir)
	} else {
		fmt.Println("\nReporte completo. Corre con opción 2 cuando quieras generar los archivos corregidos.")
	}
}
