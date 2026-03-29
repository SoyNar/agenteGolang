package web

import (
	"fmt"
	"my-agent/agent"
	"net/http"
	"os"
	"strings"
)

func Start() {
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/migrate", handleMigrate)
	http.HandleFunc("/download", handleDownload)

	fmt.Println(" Servidor web iniciado en http://localhost:8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		fmt.Printf("❌ Error iniciando servidor: %v\n", err)
	}
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<!DOCTYPE html>
<html lang="es">
<head>
    <meta charset="UTF-8">
    <title>PROTEUS</title>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: monospace; max-width: 860px; margin: 40px auto; padding: 20px; background: #1e1e1e; color: #d4d4d4; }
        h1 { color: #569cd6; margin-bottom: 8px; }
        p { color: #888; margin-bottom: 16px; }
        .upload-area { border: 2px dashed #444; padding: 20px; text-align: center; margin-bottom: 16px; cursor: pointer; }
        .upload-area:hover { border-color: #569cd6; }
        input[type=file] { display: none; }
        .actions { display: flex; align-items: center; gap: 12px; margin-bottom: 16px; }
        button { background: #0e639c; color: white; border: none; padding: 10px 24px; cursor: pointer; font-family: monospace; font-size: 14px; }
        button:hover { background: #1177bb; }
        button:disabled { background: #444; cursor: not-allowed; }
        #btnDescarga { display: none; background: #2d7a2d; color: white; padding: 10px 24px; text-decoration: none; font-family: monospace; font-size: 14px; }
        #status { color: #4ec9b0; margin-bottom: 12px; min-height: 20px; }
        #resultado { white-space: pre-wrap; background: #252526; padding: 20px; min-height: 120px; border: 1px solid #333; line-height: 1.6; }
        #fileName { color: #569cd6; margin-top: 8px; font-size: 13px; }
    </style>
</head>
<body>
    <h1>Proteus — Cambio de forma (código viejo → nuevo)</h1>
    <p>Dame un archivo .php (máximo 300 lineas) lo analizaré para migrarlo a PHP 8.1</p>

    <div class="upload-area" onclick="document.getElementById('fileInput').click()">
        Haz clic para seleccionar un archivo .php
        <div id="fileName"></div>
    </div>
    <input type="file" id="fileInput" accept=".php" onchange="mostrarNombre()">

    <div class="actions">
        <button id="btnAnalizar" onclick="migrar()">🔍 Analizar y Migrar</button>
        <a id="btnDescarga">📄 Descargar archivo migrado</a>
    </div>

    <div id="status"></div>
    <div id="resultado"></div>

    <script>
        function mostrarNombre() {
            const file = document.getElementById('fileInput').files[0];
            document.getElementById('fileName').textContent = file ? file.name : '';
        }

        async function migrar() {
            const file = document.getElementById('fileInput').files[0];
            if (!file) { alert('Selecciona un archivo .php'); return; }

            const status = document.getElementById('status');
            const resultado = document.getElementById('resultado');
            const btnDescarga = document.getElementById('btnDescarga');
            const btnAnalizar = document.getElementById('btnAnalizar');

            // Reset
            status.textContent = ' Analizando con PHPStan...';
            resultado.textContent = '';
            btnDescarga.style.display = 'none';
            btnAnalizar.disabled = true;

            const formData = new FormData();
            formData.append('file', file);

            const resp = await fetch('/migrate', { method: 'POST', body: formData });
            const reader = resp.body.getReader();
            const decoder = new TextDecoder();
            let fullText = '';

            status.textContent = ' Procesando...';

            while (true) {
                const { done, value } = await reader.read();
                if (done) break;
                fullText += decoder.decode(value);

                if (fullText.includes('[NO_MIGRATION]')) {
                    resultado.textContent = fullText.replace('[NO_MIGRATION]', '').trim();
                    status.textContent = 'El archivo ya es compatible con PHP 8.1';
                    btnAnalizar.disabled = false;
                    return;
                }

                if (fullText.includes('[DOWNLOAD_PATH]')) {
                    const match = fullText.match(/\[DOWNLOAD_PATH\](.*?)\[\/DOWNLOAD_PATH\]/);
                    if (match) {
                        btnDescarga.href = '/download?path=' + encodeURIComponent(match[1]) + '&filename=' + encodeURIComponent(file.name);
                        btnDescarga.style.display = 'inline-block';
                        resultado.textContent = fullText.replace(/\[DOWNLOAD_PATH\].*?\[\/DOWNLOAD_PATH\]/, '').trim();
                    }
                } else {
                    resultado.textContent = fullText;
                }
            }

            status.textContent = '✅ Migración completa';
            btnAnalizar.disabled = false;
        }
    </script>
</body>
</html>`)
}

func handleMigrate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Error recibiendo archivo", http.StatusBadRequest)
		return
	}
	defer file.Close()

	tmpFile, err := os.CreateTemp("", "*.php")
	if err != nil {
		http.Error(w, "Error creando archivo temporal", http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmpFile.Name())

	buf := make([]byte, 500*1024)
	n, _ := file.Read(buf)
	tmpFile.Write(buf[:n])
	tmpFile.Close()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	result, err := agent.AnalyzeFile(tmpFile.Name())
	if err != nil {
		fmt.Fprintf(w, " Error: %v", err)
		return
	}

	if !result.HasIssue {
		fmt.Fprintf(w, "%s ya es compatible con PHP 8.1, no necesita migración.", header.Filename)
		fmt.Fprintf(w, "\n[NO_MIGRATION]")
		return
	}

	errorCount := strings.Count(result.Phpstan, "at /")
	fmt.Fprintf(w, " %d problema(s) detectado(s), migrando...\n\n", errorCount)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	fixedCode, err := agent.MigrateFile(result)
	if err != nil {
		fmt.Fprintf(w, " Error migrando: %v", err)
		return
	}

	migratedPath := fmt.Sprintf("/tmp/migrated_%s", header.Filename)
	if err := os.WriteFile(migratedPath, []byte(fixedCode), 0644); err != nil {
		fmt.Fprintf(w, "Error guardando archivo: %v", err)
		return
	}

	fmt.Fprintf(w, " Migración completada exitosamente.\n")
	fmt.Fprintf(w, "[DOWNLOAD_PATH]%s[/DOWNLOAD_PATH]", migratedPath)
}

func handleDownload(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	filename := r.URL.Query().Get("filename")

	if !strings.HasPrefix(path, "/tmp/migrated_") {
		http.Error(w, "Ruta inválida", http.StatusBadRequest)
		return
	}
	if filename == "" {
		filename = "archivo_81.php"
	}

	content, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "Archivo no encontrado", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	w.Header().Set("Content-Type", "text/plain")
	w.Write(content)
}
