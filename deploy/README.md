# NexWatch — Production Deploy

## Requisitos

- VPS con Linux (Ubuntu, Debian, AlmaLinux, Rocky, RHEL, Oracle Linux)
- Nginx ya instalado y configurado con SSL en el servidor
- Docker (el script lo instala si no está)
- Dominio apuntando al VPS

## Deploy en un comando

```bash
curl -fsSL https://raw.githubusercontent.com/CogniDevAI/nexwatch/main/deploy/deploy.sh | sudo bash
```

El script instala Docker si no está, clona el repo, crea el `.env` y levanta el hub en el puerto que elijas.

## Variables de entorno (.env)

| Variable | Default | Descripción |
|---|---|---|
| `HUB_PORT` | `8090` | Puerto del host donde escucha el hub |
| `TZ` | `America/Guayaquil` | Timezone del contenedor |

## Configurar nginx

El hub expone el puerto `HUB_PORT` en localhost. Agregá esto a tu server block de nginx:

```nginx
# Necesario para WebSocket
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 443 ssl;
    server_name nexwatch.tudominio.com;

    # ... tu config SSL aquí ...

    location / {
        proxy_pass         http://127.0.0.1:8090;
        proxy_http_version 1.1;
        proxy_set_header   Upgrade $http_upgrade;
        proxy_set_header   Connection $connection_upgrade;
        proxy_set_header   Host $host;
        proxy_set_header   X-Real-IP $remote_addr;
        proxy_set_header   X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto $scheme;
        proxy_read_timeout 3600s;
    }
}
```

> El `proxy_read_timeout 3600s` es **crítico** — los agentes mantienen una conexión WebSocket persistente. Sin esto nginx la corta a los 60s y los agentes aparecen offline.

## Comandos útiles

```bash
cd /opt/nexwatch/deploy

make logs      # Ver logs en tiempo real
make status    # Estado del contenedor
make restart   # Reiniciar
make update    # Actualizar a la última versión
make down      # Apagar
```

## Agentes

Una vez que el hub está corriendo con HTTPS:

```bash
# Agente estándar
curl -fsSL https://raw.githubusercontent.com/CogniDevAI/nexwatch/main/scripts/install-agent.sh | bash -s -- \
  --hub wss://nexwatch.tudominio.com/ws/agent \
  --token TU_TOKEN

# Agente Oracle
curl -fsSL https://raw.githubusercontent.com/CogniDevAI/nexwatch/main/scripts/install-agent.sh | bash -s -- \
  --hub wss://nexwatch.tudominio.com/ws/agent \
  --token TU_TOKEN \
  --mode oracle \
  --oracle-home /u01/app/oracle/product/19.3.0/dbhome1 \
  --oracle-sid fitbank
```

> Usá `wss://` (WebSocket Secure) cuando el hub está detrás de HTTPS.
