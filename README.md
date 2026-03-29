# Prometheus 

Agente de migración automática de PHP legado a PHP 8.1 usando IA.

Prometheus recibe tu archivo PHP antiguo, lo analiza en profundidad y lo transforma a código moderno compatible con PHP 8.1, conservando la lógica original.

## ¿Qué puede migrar?

- PHP 5.x → 8.1
- PHP 7.4 → 8.1
- Cualquier versión legada intermedia

## Requisitos

- Docker
- API Key de Groq (gratis en [console.groq.com](https://console.groq.com))

## Instalación y uso

1. Clona el repositorio:
```bash
   git clone https://github.com/tu-usuario/prometheus
   cd prometheus
```

2. Crea el archivo `.env` con tu API Key:
```
   GROQ_API_KEY=tu_key_aqui
```

3. Levanta el contenedor:
```bash
   docker compose up --build
```

4. Abre tu navegador en [http://localhost:8080](http://localhost:8080)

5. Sube tu archivo PHP legado y Prometheus lo analizará y migrará automáticamente.

## ¿Qué hace Prometheus?

- Detecta la versión del código PHP recibido
- Identifica funciones y patrones deprecados
- Moderniza la sintaxis al estándar PHP 8.1
- Aplica mejoras como tipos estrictos, enums, fibers y match expressions donde corresponda
- Devuelve el archivo migrado listo para usar

## Cómo funciona internamente

Prometheus usa **LangChain** para orquestar un ciclo de migración inteligente:

1. **Análisis inicial** — PHPStan escanea el archivo y detecta problemas de compatibilidad reales.
2. **Migración con IA** — LangChain envía el código al modelo `llama-3.1-8b-instant` vía Groq con instrucciones precisas de migración.
3. **Verificación automática** — El código generado se valida nuevamente con PHPStan.
4. **Reintentos inteligentes** — Si aún hay errores, LangChain repite el ciclo hasta **3 veces**, usando cada versión corregida como base para el siguiente intento.

Este enfoque garantiza que el código migrado no solo sea sintácticamente correcto, sino que pase validación estática antes de ser entregado.