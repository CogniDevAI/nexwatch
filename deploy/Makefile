.PHONY: up down restart logs update status

COMPOSE = docker compose -f docker-compose.prod.yml

up:
	$(COMPOSE) up -d

down:
	$(COMPOSE) down

restart:
	$(COMPOSE) restart

logs:
	$(COMPOSE) logs -f --tail=100

status:
	$(COMPOSE) ps

update:
	cd .. && git pull
	$(COMPOSE) build --no-cache
	$(COMPOSE) up -d --remove-orphans

renew-ssl:
	$(COMPOSE) exec certbot certbot renew --force-renewal
	$(COMPOSE) restart nginx

shell-hub:
	$(COMPOSE) exec hub sh
