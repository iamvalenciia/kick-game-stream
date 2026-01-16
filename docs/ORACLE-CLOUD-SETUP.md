# 🚀 Oracle Cloud Free Tier - Setup Completo

## Guía paso a paso para desplegar Fight Club en Oracle Cloud ARM

> **Tu configuración actual:** US East (Ashburn) - Tenancy: iamvalenciia

---

## 📋 Tabla de Contenidos

1. [Crear la Red Virtual (VCN)](#1-crear-la-red-virtual-vcn)
2. [Crear la Instancia ARM Ampere A1](#2-crear-la-instancia-arm-ampere-a1)
3. [Configurar Reglas de Firewall](#3-configurar-reglas-de-firewall)
4. [Conectarse a la Instancia](#4-conectarse-a-la-instancia)
5. [Instalar Docker](#5-instalar-docker)
6. [Desplegar el Juego](#6-desplegar-el-juego)
7. [Configurar IP Pública Reservada](#7-configurar-ip-pública-reservada)
8. [Solución de Problemas](#8-solución-de-problemas)

---

## 1. Crear la Red Virtual (VCN)

### Paso 1.1: Navegar a Networking
```
☰ Menu (hamburguesa arriba izquierda)
  └── Networking
       └── Virtual Cloud Networks
```

### Paso 1.2: Crear VCN con Wizard
1. Click en **"Start VCN Wizard"**
2. Seleccionar **"Create VCN with Internet Connectivity"**
3. Click **"Start VCN Wizard"**

### Paso 1.3: Configurar VCN
```
┌─────────────────────────────────────────────────┐
│ Basic Information                               │
├─────────────────────────────────────────────────┤
│ VCN Name: fight-club-vcn                        │
│ Compartment: (root) - iamvalenciia              │
├─────────────────────────────────────────────────┤
│ Configure VCN and Subnets                       │
├─────────────────────────────────────────────────┤
│ VCN IPv4 CIDR Block: 10.0.0.0/16               │
│ Public Subnet CIDR:  10.0.0.0/24               │
│ Private Subnet CIDR: 10.0.1.0/24               │
└─────────────────────────────────────────────────┘
```

4. Click **"Next"** → Review → **"Create"**
5. Esperar ~30 segundos hasta ver ✅ "Virtual Cloud Network created"

---

## 2. Crear la Instancia ARM Ampere A1

### Paso 2.1: Navegar a Compute
```
☰ Menu (hamburguesa)
  └── Compute
       └── Instances
```

### Paso 2.2: Crear Instancia
1. Click en **"Create instance"** (botón azul)

### Paso 2.3: Configuración de la Instancia

#### Nombre y Compartment
```
┌─────────────────────────────────────────────────┐
│ Name: fight-club-server                         │
│ Compartment: (root) - iamvalenciia              │
└─────────────────────────────────────────────────┘
```

#### Placement (dejar por defecto)
- Availability Domain: AD-1 (o el disponible)

#### Image and Shape (⚠️ MUY IMPORTANTE)

1. Click en **"Edit"** en la sección "Image and shape"

2. **Seleccionar Imagen:**
   - Click en **"Change image"**
   - Seleccionar: **Canonical Ubuntu 22.04** ✅
   - (O también puedes usar Oracle Linux 8)

3. **Seleccionar Shape (CRÍTICO):**
   - Click en **"Change shape"**
   - Seleccionar tab: **"Ampere"** (ARM)
   - Shape name: **VM.Standard.A1.Flex**
   - **Number of OCPUs:** `4` (máximo gratis)
   - **Amount of memory (GB):** `24` (máximo gratis)

```
┌─────────────────────────────────────────────────┐
│ SHAPE CONFIGURATION                             │
├─────────────────────────────────────────────────┤
│ Shape series: Ampere                            │
│ Shape name:   VM.Standard.A1.Flex               │
│                                                 │
│ ┌─────────────────────────────────────────────┐ │
│ │ Number of OCPUs:     [====4====]           │ │
│ │ Amount of memory:    [====24===] GB        │ │
│ └─────────────────────────────────────────────┘ │
│                                                 │
│ 💰 Estimated cost: Always Free eligible         │
└─────────────────────────────────────────────────┘
```

#### Networking
1. Click **"Edit"** en "Networking"
2. Configurar:
```
┌─────────────────────────────────────────────────┐
│ Virtual cloud network: fight-club-vcn           │
│ Subnet: Public Subnet-fight-club-vcn (regional) │
│ Public IPv4 address: ☑️ Assign a public IPv4    │
└─────────────────────────────────────────────────┘
```

#### Add SSH Keys (⚠️ MUY IMPORTANTE)

**Opción A: Generar nuevas llaves (Recomendado si no tienes)**
1. Seleccionar **"Generate a key pair for me"**
2. Click en **"Save private key"** → Guarda el archivo `.key`
3. Click en **"Save public key"** → Guarda el archivo `.pub`

**Opción B: Usar llaves existentes**
1. Seleccionar **"Upload public key files (.pub)"**
2. Subir tu archivo `~/.ssh/id_rsa.pub`

```
⚠️ IMPORTANTE: Guarda las llaves SSH en un lugar seguro.
Sin ellas NO podrás acceder a tu servidor.
```

#### Boot Volume
```
┌─────────────────────────────────────────────────┐
│ Boot volume size: 100 GB (máximo gratis)        │
│ ☑️ Use in-transit encryption                    │
└─────────────────────────────────────────────────┘
```

### Paso 2.4: Crear
1. Click en **"Create"** (botón azul abajo)
2. Esperar 2-5 minutos hasta que el estado sea **RUNNING** ✅

```
┌─────────────────────────────────────────────────┐
│ Instance: fight-club-server                     │
│ State: 🟢 RUNNING                               │
│ Public IP: 129.xxx.xxx.xxx                      │
│ Shape: VM.Standard.A1.Flex                      │
│ OCPU: 4    Memory: 24 GB                        │
└─────────────────────────────────────────────────┘
```

---

## 3. Configurar Reglas de Firewall

### Paso 3.1: Abrir Security List
```
☰ Menu
  └── Networking
       └── Virtual Cloud Networks
            └── fight-club-vcn
                 └── Security Lists
                      └── Default Security List for fight-club-vcn
```

### Paso 3.2: Agregar Ingress Rules
Click en **"Add Ingress Rules"** y agregar estas reglas:

#### Regla 1: Puerto 3000 (API del juego)
```
┌─────────────────────────────────────────────────┐
│ Source Type: CIDR                               │
│ Source CIDR: 0.0.0.0/0                          │
│ IP Protocol: TCP                                │
│ Destination Port Range: 3000                    │
│ Description: Fight Club API                     │
└─────────────────────────────────────────────────┘
```

#### Regla 2: Puerto 80 (HTTP - opcional)
```
┌─────────────────────────────────────────────────┐
│ Source Type: CIDR                               │
│ Source CIDR: 0.0.0.0/0                          │
│ IP Protocol: TCP                                │
│ Destination Port Range: 80                      │
│ Description: HTTP                               │
└─────────────────────────────────────────────────┘
```

#### Regla 3: Puerto 443 (HTTPS - opcional)
```
┌─────────────────────────────────────────────────┐
│ Source Type: CIDR                               │
│ Source CIDR: 0.0.0.0/0                          │
│ IP Protocol: TCP                                │
│ Destination Port Range: 443                     │
│ Description: HTTPS                              │
└─────────────────────────────────────────────────┘
```

### Paso 3.3: Verificar Reglas
Deberías ver estas reglas en la lista:
```
┌────────────────────────────────────────────────────────────┐
│ Ingress Rules                                              │
├──────────────┬──────────┬────────────────┬─────────────────┤
│ Source       │ Protocol │ Dest Port      │ Description     │
├──────────────┼──────────┼────────────────┼─────────────────┤
│ 0.0.0.0/0    │ TCP      │ 22             │ SSH (default)   │
│ 0.0.0.0/0    │ TCP      │ 3000           │ Fight Club API  │
│ 0.0.0.0/0    │ TCP      │ 80             │ HTTP            │
│ 0.0.0.0/0    │ TCP      │ 443            │ HTTPS           │
└──────────────┴──────────┴────────────────┴─────────────────┘
```

---

## 4. Conectarse a la Instancia

### Paso 4.1: Obtener IP Pública
```
☰ Menu → Compute → Instances → fight-club-server
```
Copiar la **Public IP address** (ej: `129.153.xxx.xxx`)

### Paso 4.2: Conectar via SSH

**Desde Linux/Mac:**
```bash
# Si descargaste la llave de Oracle
chmod 400 ~/Downloads/ssh-key-*.key
ssh -i ~/Downloads/ssh-key-*.key ubuntu@TU_IP_PUBLICA

# Si usaste tu llave existente
ssh ubuntu@TU_IP_PUBLICA
```

**Desde Windows (PowerShell):**
```powershell
ssh -i C:\Users\TuUsuario\Downloads\ssh-key-*.key ubuntu@TU_IP_PUBLICA
```

**Desde Windows (PuTTY):**
1. Convertir `.key` a `.ppk` con PuTTYgen
2. Usar PuTTY con la IP y el archivo `.ppk`

### Paso 4.3: Verificar Conexión
```bash
# Deberías ver algo como:
ubuntu@fight-club-server:~$

# Verificar arquitectura ARM
uname -m
# Output: aarch64 ✅
```

---

## 5. Instalar Docker

### Paso 5.1: Actualizar Sistema
```bash
sudo apt update && sudo apt upgrade -y
```

### Paso 5.2: Instalar Docker
```bash
# Instalar dependencias
sudo apt install -y apt-transport-https ca-certificates curl software-properties-common

# Agregar GPG key de Docker
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg

# Agregar repositorio
echo "deb [arch=arm64 signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

# Instalar Docker
sudo apt update
sudo apt install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

# Agregar usuario al grupo docker
sudo usermod -aG docker $USER

# Aplicar cambios de grupo (o reconectar SSH)
newgrp docker
```

### Paso 5.3: Verificar Docker
```bash
docker --version
# Docker version 24.x.x, build xxxxx

docker compose version
# Docker Compose version v2.x.x
```

---

## 6. Desplegar el Juego

### Paso 6.1: Clonar Repositorio
```bash
cd ~
git clone https://github.com/iamvalenciia/kick-game-stream.git
cd kick-game-stream
```

### Paso 6.2: Configurar Variables de Entorno
```bash
# Copiar template
cp .env.example .env

# Editar con tus credenciales
nano .env
```

**Contenido de `.env`:**
```bash
# Kick Streaming
STREAM_KEY_KICK=tu_stream_key
CLIENT_ID_KICK=tu_client_id
CLIENT_SECRET_KICK=tu_client_secret
KICK_BROADCASTER_USER_ID=tu_user_id

# Server
PORT=3000
PUBLIC_URL=http://TU_IP_PUBLICA:3000

# Game Config
MAX_PLAYERS=100
GAME_TICK_RATE=30
STREAM_FPS=30
STREAM_BITRATE=4500
MUSIC_ENABLED=true
MUSIC_VOLUME=0.15

# Opcional: Cloud Services
# REDIS_PUBLIC_ENDPOINT=
# REDIS_PASSWORD=
# MONGODB_CLUSTER_USERNAME=
# MONGODB_CLUSTER_PASSWORD=
```

### Paso 6.3: Build y Deploy
```bash
# Build para ARM64
docker compose build

# Iniciar en background
docker compose up -d

# Ver logs
docker compose logs -f
```

### Paso 6.4: Verificar
```bash
# Verificar que está corriendo
docker compose ps

# Probar API
curl http://localhost:3000/api/state

# Ver desde fuera (usa tu IP pública)
# http://TU_IP_PUBLICA:3000/admin
```

---

## 7. Configurar IP Pública Reservada

Para que tu IP no cambie si reinicias la instancia:

### Paso 7.1: Crear Reserved IP
```
☰ Menu
  └── Networking
       └── IP Management
            └── Reserved Public IPs
```

### Paso 7.2: Reservar IP
1. Click **"Reserve Public IP Address"**
2. Configurar:
```
┌─────────────────────────────────────────────────┐
│ Name: fight-club-ip                             │
│ Compartment: (root)                             │
│ IP Address Source: ☉ Oracle                     │
└─────────────────────────────────────────────────┘
```
3. Click **"Reserve Public IP Address"**

### Paso 7.3: Asignar a Instancia
1. Click en la IP reservada
2. Click **"Edit"** → **"Assign to instance"**
3. Seleccionar: `fight-club-server`
4. VNIC: Primary VNIC
5. Click **"Assign"**

---

## 8. Solución de Problemas

### ❌ "Out of capacity" al crear instancia
```
Solución:
1. Intentar en diferentes Availability Domains (AD-1, AD-2, AD-3)
2. Intentar con menos recursos (2 OCPU, 12GB RAM)
3. Intentar en horarios de menor demanda (madrugada)
4. Esperar unos días y reintentar
```

### ❌ No puedo conectar por SSH
```
Verificar:
1. Security List tiene regla para puerto 22
2. La llave SSH es correcta
3. Usuario correcto: ubuntu (Ubuntu) o opc (Oracle Linux)
```

### ❌ Puerto 3000 no accesible
```
Verificar:
1. Security List en Oracle tiene puerto 3000 abierto
2. Firewall del OS:
   sudo iptables -I INPUT -p tcp --dport 3000 -j ACCEPT
   # O si usas firewalld:
   sudo firewall-cmd --permanent --add-port=3000/tcp
   sudo firewall-cmd --reload
```

### ❌ Docker build falla
```
Verificar:
1. Arquitectura: uname -m (debe ser aarch64)
2. Dockerfile usa imagen multi-arch
3. Suficiente espacio: df -h
```

### ❌ Stream no inicia
```
Verificar:
1. STREAM_KEY_KICK es correcto
2. FFmpeg está instalado: docker exec -it game-server ffmpeg -version
3. Conexión a Kick: docker compose logs | grep -i rtmp
```

---

## 📊 Recursos Utilizados (Free Tier)

| Recurso | Usado | Disponible | Estado |
|---------|-------|------------|--------|
| ARM OCPU | 4 | 4 | ✅ Máximo |
| RAM | 24 GB | 24 GB | ✅ Máximo |
| Boot Volume | 100 GB | 200 GB | 🟡 50% |
| Outbound Data | Variable | 10 TB/mes | ✅ OK |
| Reserved IP | 1 | 1 | ✅ Usado |

---

## 🎮 Comandos Útiles

```bash
# Ver estado
docker compose ps

# Ver logs en tiempo real
docker compose logs -f

# Reiniciar servicio
docker compose restart

# Actualizar (después de git pull)
docker compose down
docker compose build --no-cache
docker compose up -d

# Ver uso de recursos
docker stats

# Entrar al contenedor
docker compose exec game-server sh
```

---

## ✅ Checklist Final

- [ ] VCN creada con subnets públicas
- [ ] Instancia ARM A1 corriendo (4 OCPU, 24GB RAM)
- [ ] Security List con puertos 22, 3000, 80, 443
- [ ] Conexión SSH funcionando
- [ ] Docker instalado
- [ ] Variables de entorno configuradas
- [ ] Contenedor corriendo
- [ ] Stream activo en Kick
- [ ] IP reservada asignada

---

**¡Tu juego ahora está corriendo 24/7 GRATIS en Oracle Cloud!** 🎉
