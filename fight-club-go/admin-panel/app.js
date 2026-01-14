/**
 * Fight Club Admin Panel - Go Backend Compatible
 * Uses native WebSocket instead of Socket.IO
 */

class FightClubAdmin {
    constructor() {
        this.ws = null;
        this.players = [];
        this.streamStats = null;
        this.reconnectAttempts = 0;
        this.maxReconnectAttempts = 5;

        this.init();
    }

    init() {
        this.bindEvents();
        this.connectWebSocket();
        this.startPolling();
    }

    // WebSocket connection
    connectWebSocket() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/ws`;

        console.log('🔌 Connecting to WebSocket:', wsUrl);

        this.ws = new WebSocket(wsUrl);

        this.ws.onopen = () => {
            console.log('✅ WebSocket connected');
            this.reconnectAttempts = 0;
            this.updateConnectionStatus(true);
        };

        this.ws.onmessage = (event) => {
            try {
                const msg = JSON.parse(event.data);
                this.handleMessage(msg);
            } catch (e) {
                console.error('Failed to parse message:', e);
            }
        };

        this.ws.onclose = () => {
            console.log('❌ WebSocket disconnected');
            this.updateConnectionStatus(false);
            this.scheduleReconnect();
        };

        this.ws.onerror = (error) => {
            console.error('WebSocket error:', error);
        };
    }

    scheduleReconnect() {
        if (this.reconnectAttempts < this.maxReconnectAttempts) {
            this.reconnectAttempts++;
            const delay = Math.min(1000 * this.reconnectAttempts, 5000);
            console.log(`🔄 Reconnecting in ${delay}ms (attempt ${this.reconnectAttempts})`);
            setTimeout(() => this.connectWebSocket(), delay);
        }
    }

    handleMessage(msg) {
        switch (msg.event) {
            case 'game:state':
                this.updateGameState(msg.data);
                break;
            case 'stream:stats':
                this.updateStreamStats(msg.data);
                break;
            default:
                console.log('Unknown event:', msg.event);
        }
    }

    updateConnectionStatus(connected) {
        const indicator = document.getElementById('connection-status');
        if (indicator) {
            indicator.textContent = connected ? '🟢 Connected' : '🔴 Disconnected';
            indicator.className = connected ? 'connected' : 'disconnected';
        }
    }

    // Polling fallback for initial data
    startPolling() {
        this.fetchStats();
        this.fetchKickStatus();
        setInterval(() => this.fetchStats(), 5000);
        setInterval(() => this.fetchKickStatus(), 5000);
    }

    async fetchKickStatus() {
        try {
            const response = await fetch('/api/kick/status');
            const data = await response.json();
            this.updateKickStatus(data.connected);
        } catch (e) {
            this.updateKickStatus(false);
        }
    }

    updateKickStatus(connected) {
        const indicator = document.getElementById('kick-indicator');
        const statusText = document.getElementById('kick-status-text');
        const authBtn = document.getElementById('kick-auth-btn');

        if (indicator) {
            indicator.style.background = connected ? '#4ecdc4' : '#ff6b6b';
        }
        if (statusText) {
            statusText.textContent = connected ? 'Connected ✓' : 'Not Connected';
            statusText.style.color = connected ? '#4ecdc4' : '#ff6b6b';
        }
        if (authBtn) {
            if (connected) {
                authBtn.textContent = '✅ Bot Connected';
                authBtn.style.background = 'linear-gradient(135deg, #4ecdc4, #44bd9f)';
            } else {
                authBtn.textContent = '🤖 Login as Bot';
                authBtn.style.background = 'linear-gradient(135deg, #53fc18, #41c911)';
            }
        }
    }

    async fetchStats() {
        try {
            const response = await fetch('/api/stats');
            const data = await response.json();
            this.updateStats(data);
        } catch (e) {
            console.error('Failed to fetch stats:', e);
        }
    }

    // UI Updates
    updateGameState(data) {
        this.players = data.players || [];
        this.renderPlayers();
        this.updatePlayerCount(data.playerCount, data.aliveCount);
    }

    updateStreamStats(data) {
        this.streamStats = data;
        this.renderStreamStats();
    }

    updateStats(data) {
        const playerCount = document.getElementById('player-count');
        const aliveCount = document.getElementById('alive-count');
        const killCount = document.getElementById('kill-count');

        if (playerCount) playerCount.textContent = data.playerCount || 0;
        if (aliveCount) aliveCount.textContent = data.aliveCount || 0;
        if (killCount) killCount.textContent = data.totalKills || 0;

        // Stream status
        const streamBtn = document.getElementById('stream-toggle');
        if (streamBtn) {
            if (data.streaming) {
                streamBtn.textContent = '🔴 Stop Stream';
                streamBtn.classList.add('streaming');
            } else {
                streamBtn.textContent = '▶️ Start Stream';
                streamBtn.classList.remove('streaming');
            }
        }
    }

    updatePlayerCount(total, alive) {
        const playerCount = document.getElementById('player-count');
        const aliveCount = document.getElementById('alive-count');

        if (playerCount) playerCount.textContent = total || 0;
        if (aliveCount) aliveCount.textContent = alive || 0;
    }

    renderPlayers() {
        const container = document.getElementById('players-list');
        if (!container) return;

        // Sort by kills
        const sorted = [...this.players].sort((a, b) => b.kills - a.kills);

        container.innerHTML = sorted.slice(0, 20).map((p, i) => `
            <div class="player-item ${p.isDead ? 'dead' : 'alive'}">
                <span class="rank">#${i + 1}</span>
                <span class="name" style="color: ${p.color}">${p.name}</span>
                <span class="kills">⚔️ ${p.kills}</span>
                <span class="money">💰 $${p.money}</span>
                <span class="hp">❤️ ${p.hp}/${p.maxHp}</span>
            </div>
        `).join('');
    }

    renderStreamStats() {
        const container = document.getElementById('stream-stats');
        if (!container || !this.streamStats) return;

        container.innerHTML = `
            <div class="stat">
                <label>Status:</label>
                <span class="${this.streamStats.streaming ? 'live' : 'offline'}">
                    ${this.streamStats.streaming ? '🔴 LIVE' : '⚫ Offline'}
                </span>
            </div>
            <div class="stat">
                <label>Frames:</label>
                <span>${this.streamStats.framesSent || 0}</span>
            </div>
            <div class="stat">
                <label>FPS:</label>
                <span>${this.streamStats.actualFps || '0'}</span>
            </div>
            <div class="stat">
                <label>Uptime:</label>
                <span>${this.streamStats.uptime || '0s'}</span>
            </div>
            <div class="stat">
                <label>Resolution:</label>
                <span>${this.streamStats.resolution || '1920x1080'}</span>
            </div>
        `;
    }

    // Event handlers
    bindEvents() {
        // Stream toggle
        document.addEventListener('click', async (e) => {
            if (e.target.id === 'stream-toggle') {
                const isStreaming = e.target.classList.contains('streaming');
                const endpoint = isStreaming ? '/api/stream/stop' : '/api/stream/start';

                try {
                    e.target.disabled = true;
                    e.target.textContent = '⏳ Please wait...';

                    const response = await fetch(endpoint, { method: 'POST' });
                    const data = await response.json();

                    if (data.success) {
                        console.log(isStreaming ? 'Stream stopped' : 'Stream started');
                    } else {
                        alert('Failed: ' + (data.error || 'Unknown error'));
                    }

                    this.fetchStats();
                } catch (error) {
                    console.error('Stream toggle error:', error);
                    alert('Failed to toggle stream');
                } finally {
                    e.target.disabled = false;
                }
            }

            // Add player
            if (e.target.id === 'add-player-btn') {
                const nameInput = document.getElementById('player-name');
                const name = nameInput ? nameInput.value.trim() : '';

                if (name) {
                    try {
                        await fetch('/api/player/join', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ name })
                        });
                        if (nameInput) nameInput.value = '';
                    } catch (e) {
                        console.error('Failed to add player:', e);
                    }
                }
            }

            // Batch Add Players
            if (e.target.classList.contains('batch-btn')) {
                const count = parseInt(e.target.getAttribute('data-count'));
                if (count > 0) {
                    try {
                        const originalText = e.target.textContent;
                        e.target.textContent = '⏳';
                        e.target.disabled = true;

                        await fetch('/api/player/batch', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ count })
                        });

                        // Visual feedback
                        setTimeout(() => {
                            e.target.textContent = '✅';
                            setTimeout(() => {
                                e.target.textContent = originalText;
                                e.target.disabled = false;
                            }, 1000);
                        }, 500);

                    } catch (err) {
                        console.error('Failed to batch add players:', err);
                        e.target.textContent = '❌';
                        setTimeout(() => {
                            e.target.disabled = false;
                            e.target.textContent = e.target.getAttribute('data-count');
                        }, 1000);
                    }
                }
            }
        });
    }
}

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', () => {
    window.admin = new FightClubAdmin();
});
