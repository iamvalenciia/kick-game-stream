# 🔄 CI/CD Pipeline - Fight Club

## Guía completa para Continuous Integration y Continuous Deployment

> Pipeline automatizado: GitHub → GitHub Container Registry → Oracle Cloud ARM

---

## 📋 Tabla de Contenidos

1. [Visión General](#1-visión-general)
2. [Arquitectura del Pipeline](#2-arquitectura-del-pipeline)
3. [Configuración Inicial](#3-configuración-inicial)
4. [Configurar GitHub Secrets](#4-configurar-github-secrets)
5. [Flujo de Trabajo](#5-flujo-de-trabajo)
6. [Despliegue Manual](#6-despliegue-manual)
7. [Monitoreo y Logs](#7-monitoreo-y-logs)
8. [Rollback](#8-rollback)
9. [Troubleshooting](#9-troubleshooting)

---

## 1. Visión General

### ¿Qué hace este CI/CD?

```
┌─────────────────────────────────────────────────────────────────────┐
│                         FLUJO CI/CD                                  │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   [Tu Código]  →  [GitHub]  →  [Tests]  →  [Build]  →  [Deploy]    │
│       │              │            │           │            │         │
│       │              │            │           │            │         │
│   git push      Trigger       Go Test    Docker      SSH to         │
│                 Action                   Multi-arch   Oracle        │
│                                          arm64+amd64                │
│                                              │                       │
│                                              ↓                       │
│                                    [GitHub Container                 │
│                                       Registry]                      │
│                                         FREE!                        │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Beneficios

| Beneficio | Descripción |
|-----------|-------------|
| **Automatizado** | Push a `main` = Deploy automático |
| **Gratis** | GitHub Actions + GHCR = $0 |
| **Seguro** | Secrets encriptados, SSH key auth |
| **Rápido** | Build cacheado, ~5-10 min total |
| **Multi-arch** | Imagen ARM64 + AMD64 |
| **Rollback** | Fácil volver a versiones anteriores |

---

## 2. Arquitectura del Pipeline

### Workflows Disponibles

```
.github/workflows/
├── deploy.yml      # Pipeline principal (push to main)
└── pr-check.yml    # Verificación de PRs (tests + build check)
```

### Pipeline Principal (`deploy.yml`)

```
┌──────────────────────────────────────────────────────────────────┐
│                        deploy.yml                                 │
├──────────────────────────────────────────────────────────────────┤
│                                                                   │
│  ┌─────────┐     ┌─────────┐     ┌─────────┐     ┌─────────┐    │
│  │  TEST   │ ──▶ │  BUILD  │ ──▶ │ DEPLOY  │ ──▶ │ CLEANUP │    │
│  └─────────┘     └─────────┘     └─────────┘     └─────────┘    │
│      │               │               │               │           │
│      │               │               │               │           │
│   Go test       Docker           SSH to          Delete old     │
│   Go race       buildx           Oracle          images         │
│   Coverage      Multi-arch       Pull image     (keep 5)        │
│                 Push GHCR        Restart                         │
│                                                                   │
└──────────────────────────────────────────────────────────────────┘
```

### Pipeline de PR (`pr-check.yml`)

```
┌──────────────────────────────────────────────────────────────────┐
│                       pr-check.yml                                │
├──────────────────────────────────────────────────────────────────┤
│                                                                   │
│  ┌─────────┐     ┌─────────────┐     ┌─────────┐                 │
│  │  TEST   │     │ BUILD CHECK │     │  LINT   │  (parallel)     │
│  └─────────┘     └─────────────┘     └─────────┘                 │
│      │                 │                  │                       │
│   Go test         Build ARM64       golangci-lint                │
│   Coverage        (no push)         (optional)                    │
│                                                                   │
└──────────────────────────────────────────────────────────────────┘
```

---

## 3. Configuración Inicial

### Paso 3.1: Prerequisitos

- ✅ Cuenta de GitHub con el repositorio
- ✅ Instancia Oracle Cloud ARM configurada (ver `ORACLE-CLOUD-SETUP.md`)
- ✅ Docker instalado en Oracle Cloud
- ✅ SSH key para acceder a Oracle

### Paso 3.2: Preparar Oracle Cloud para CI/CD

Conecta a tu instancia Oracle y ejecuta:

```bash
# 1. Clonar repositorio (si no lo has hecho)
cd ~
git clone https://github.com/iamvalenciia/kick-game-stream.git
cd kick-game-stream

# 2. Configurar .env
cp .env.example .env
nano .env  # Editar con tus credenciales

# 3. Login a GitHub Container Registry
# Crear Personal Access Token en: https://github.com/settings/tokens
# Permisos necesarios: read:packages, write:packages
docker login ghcr.io -u TU_USUARIO_GITHUB
# Password: tu Personal Access Token

# 4. Verificar login
cat ~/.docker/config.json
```

### Paso 3.3: Habilitar GitHub Actions

1. Ve a tu repositorio en GitHub
2. Click en **Settings** → **Actions** → **General**
3. En "Actions permissions", seleccionar **"Allow all actions"**
4. En "Workflow permissions", seleccionar **"Read and write permissions"**
5. Click **"Save"**

---

## 4. Configurar GitHub Secrets

### Paso 4.1: Navegar a Secrets

```
GitHub Repo → Settings → Secrets and variables → Actions
```

### Paso 4.2: Crear Repository Secrets

Click en **"New repository secret"** para cada uno:

| Secret Name | Valor | Descripción |
|-------------|-------|-------------|
| `ORACLE_HOST` | `129.xxx.xxx.xxx` | IP pública de tu instancia Oracle |
| `ORACLE_USER` | `ubuntu` | Usuario SSH (ubuntu para Ubuntu, opc para Oracle Linux) |
| `ORACLE_SSH_KEY` | `-----BEGIN OPENSSH...` | Contenido completo de tu llave privada SSH |

### Paso 4.3: Obtener SSH Key

```bash
# En tu máquina local, donde tienes la llave de Oracle
cat ~/.ssh/tu_llave_oracle.key

# O si usaste la llave generada por Oracle
cat ~/Downloads/ssh-key-*.key
```

Copia TODO el contenido (incluyendo `-----BEGIN` y `-----END`) y pégalo en el secret `ORACLE_SSH_KEY`.

### Paso 4.4: Crear Environment (Opcional pero recomendado)

1. Ve a **Settings** → **Environments**
2. Click **"New environment"**
3. Nombre: `production`
4. Agregar protection rules (opcional):
   - ✅ Required reviewers
   - ✅ Wait timer (0-30 min)

---

## 5. Flujo de Trabajo

### Desarrollo Normal

```bash
# 1. Crear rama para tu feature
git checkout -b feature/nueva-funcionalidad

# 2. Hacer cambios
# ... editar código ...

# 3. Commit y push
git add .
git commit -m "feat: agregar nueva funcionalidad"
git push -u origin feature/nueva-funcionalidad

# 4. Crear Pull Request en GitHub
# → Esto ejecuta pr-check.yml (tests + build check)

# 5. Mergear PR a main
# → Esto ejecuta deploy.yml (tests → build → deploy)
```

### Diagrama de Flujo

```
                    ┌─────────────────┐
                    │  Feature Branch │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │  Pull Request   │
                    └────────┬────────┘
                             │
              ┌──────────────┼──────────────┐
              │              │              │
              ▼              ▼              ▼
         ┌────────┐    ┌──────────┐   ┌────────┐
         │  TEST  │    │BUILD CHK │   │  LINT  │
         └────┬───┘    └────┬─────┘   └────┬───┘
              │             │              │
              └──────────────┼──────────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │  PR Approved ✅  │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │  Merge to main  │
                    └────────┬────────┘
                             │
              ┌──────────────┼──────────────┐
              │              │              │
              ▼              ▼              ▼
         ┌────────┐    ┌──────────┐   ┌─────────┐
         │  TEST  │───▶│  BUILD   │──▶│ DEPLOY  │
         └────────┘    └──────────┘   └─────────┘
                             │              │
                             ▼              ▼
                       ┌──────────┐   ┌──────────────┐
                       │   GHCR   │   │ Oracle Cloud │
                       └──────────┘   └──────────────┘
```

---

## 6. Despliegue Manual

### Opción A: Trigger Manual desde GitHub

1. Ve a **Actions** → **Build & Deploy**
2. Click **"Run workflow"**
3. Selecciona branch: `main`
4. Opciones:
   - `skip_tests`: Saltar tests (usar con cuidado)
   - `force_deploy`: Forzar deploy aunque fallen tests
5. Click **"Run workflow"**

### Opción B: Deploy desde Terminal (Oracle)

```bash
# Conectar a Oracle
ssh ubuntu@TU_IP

# Ir al directorio
cd ~/kick-game-stream

# Usar script de deploy
./scripts/deploy-oracle.sh update

# O manualmente:
git pull origin main
docker pull ghcr.io/iamvalenciia/kick-game-stream:latest
docker compose -f docker-compose.prod.yml down
docker compose -f docker-compose.prod.yml up -d
```

### Opción C: Deploy con Build Local

```bash
# En Oracle Cloud
cd ~/kick-game-stream
git pull origin main

# Build local (más lento pero no requiere GHCR)
docker compose build
docker compose up -d
```

---

## 7. Monitoreo y Logs

### Ver Pipeline en GitHub

```
Repo → Actions → Click en el workflow run
```

### Ver Logs en Oracle

```bash
# Logs en tiempo real
docker compose logs -f

# Logs del último deploy
docker compose logs --tail=100

# Estado de containers
docker compose ps

# Uso de recursos
docker stats
```

### Health Check

```bash
# Verificar que la API responde
curl http://localhost:3000/api/state

# Verificar desde fuera
curl http://TU_IP_PUBLICA:3000/api/state
```

---

## 8. Rollback

### Opción A: Rollback a imagen anterior

```bash
# Ver tags disponibles
docker images ghcr.io/iamvalenciia/kick-game-stream

# O listar en GHCR
# https://github.com/iamvalenciia/kick-game-stream/pkgs/container/kick-game-stream

# Desplegar versión específica
export IMAGE_TAG=ghcr.io/iamvalenciia/kick-game-stream:abc1234
docker compose -f docker-compose.prod.yml down
docker compose -f docker-compose.prod.yml up -d
```

### Opción B: Rollback con git

```bash
# Ver commits recientes
git log --oneline -10

# Volver a commit específico
git checkout abc1234

# Rebuild
docker compose build
docker compose up -d
```

---

## 9. Troubleshooting

### ❌ Pipeline falla en "Test"

```
Causa: Tests de Go fallan
Solución:
1. Ver logs del job en GitHub Actions
2. Ejecutar tests localmente: cd fight-club-go && go test ./...
3. Corregir errores y push de nuevo
```

### ❌ Pipeline falla en "Build"

```
Causa: Dockerfile tiene errores o dependencias fallan
Solución:
1. Build local: docker compose build
2. Verificar Dockerfile
3. Revisar logs de build en GitHub Actions
```

### ❌ Pipeline falla en "Deploy"

```
Causa: SSH connection falla o Docker falla en Oracle
Soluciones:
1. Verificar secrets ORACLE_HOST, ORACLE_USER, ORACLE_SSH_KEY
2. Verificar que el servidor Oracle está corriendo
3. Conectar manualmente: ssh -i key ubuntu@IP
4. Verificar Docker en Oracle: docker ps
```

### ❌ "Permission denied" en SSH

```
Solución:
1. Verificar que la llave SSH es correcta (toda la llave, incluyendo headers)
2. En Oracle, verificar: cat ~/.ssh/authorized_keys
3. La llave pública debe estar ahí
```

### ❌ "Unauthorized" al pull image

```
Solución en Oracle:
1. docker logout ghcr.io
2. docker login ghcr.io -u TU_USUARIO
   Password: Personal Access Token (con permisos read:packages)
3. docker pull ghcr.io/iamvalenciia/kick-game-stream:latest
```

### ❌ Container no inicia

```bash
# Ver logs del container
docker compose logs game-server

# Errores comunes:
# - .env no existe → cp .env.example .env
# - Puerto ocupado → docker ps -a, docker rm container_viejo
# - Permisos → sudo chown -R $USER:$USER ~/kick-game-stream
```

---

## 📊 Comandos Rápidos

```bash
# === En tu máquina local ===
git push origin main                    # Trigger deploy
git push origin feature/xyz             # Solo PR checks

# === En Oracle Cloud ===
./scripts/deploy-oracle.sh status       # Ver estado
./scripts/deploy-oracle.sh logs         # Ver logs
./scripts/deploy-oracle.sh restart      # Reiniciar
./scripts/deploy-oracle.sh update       # Actualizar manualmente

# === Docker ===
docker compose ps                       # Estado containers
docker compose logs -f                  # Logs en vivo
docker compose down && docker compose up -d  # Reiniciar
docker system prune -a                  # Limpiar espacio
```

---

## 🎯 Checklist de Setup

- [ ] Oracle Cloud instance corriendo (4 OCPU, 24GB RAM)
- [ ] Docker instalado en Oracle
- [ ] SSH key configurada
- [ ] GitHub Actions habilitado
- [ ] Secret `ORACLE_HOST` configurado
- [ ] Secret `ORACLE_USER` configurado
- [ ] Secret `ORACLE_SSH_KEY` configurado
- [ ] Environment `production` creado (opcional)
- [ ] Docker login a GHCR en Oracle
- [ ] `.env` configurado en Oracle
- [ ] Primer push a main exitoso
- [ ] Stream funcionando en Kick

---

## 🔗 Links Útiles

- [GitHub Actions Docs](https://docs.github.com/en/actions)
- [GitHub Container Registry](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)
- [Docker Buildx Multi-arch](https://docs.docker.com/build/building/multi-platform/)
- [Oracle Cloud Free Tier](https://www.oracle.com/cloud/free/)

---

**¡Tu pipeline CI/CD está listo!** Cada push a `main` desplegará automáticamente. 🚀
