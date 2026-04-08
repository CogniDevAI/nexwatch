# NexWatch — Production Deploy

## Requisitos

- VPS con Ubuntu 22.04 o Debian 12 (mínimo 1 vCPU, 1 GB RAM)
- Dominio apuntando al VPS en Cloudflare (registro A con proxy **desactivado** ☁️→🟠 durante el primer deploy para que Let's Encrypt pueda validar)
- Puertos 80 y 443 abiertos en el firewall del VPS

## Deploy en un comando

```bash
curl -fsSL https://raw.githubusercontent.com/CogniDevAI/nexwatch/main/deploy/deploy.sh | sudo bash
```

El script instala Docker, clona el repo, configura SSL automáticamente y levanta todo.

## Deploy manual

```bash
git clone https://github.com/CogniDevAI/nexwatch.git /opt/nexwatch
cd /opt/nexwatch/deploy
cp .env.example .env
# Editar .env con tu dominio, email y timezone
nano .env
bash deploy.sh
```

## Cloudflare

1. Durante el primer deploy, el proxy de Cloudflare debe estar **desactivado** (nube gris) para que Let's Encrypt valide el dominio
2. Una vez que el certificado esté emitido, podés activar el proxy (nube naranja)
3. En Cloudflare → SSL/TLS → configurar modo **Full (strict)**

## Actualizar

```bash
cd /opt/nexwatch/deploy
make update
```

## Comandos útiles

```bash
make logs      # Ver logs en tiempo real
make status    # Estado de los contenedores
make restart   # Reiniciar servicios
make down      # Apagar todo
make renew-ssl # Forzar renovación SSL
```

## Agentes

Una vez que el hub está corriendo, los agentes apuntan al dominio público:

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

> Nota: usá `wss://` (WebSocket Secure) cuando el hub está detrás de HTTPS.
