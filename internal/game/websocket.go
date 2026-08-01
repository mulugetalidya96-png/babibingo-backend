package game

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Add error handler
	Error: func(w http.ResponseWriter, r *http.Request, status int, reason error) {
		log.Printf("[WS] Upgrade error: %v", reason)
	},
}

type WSClient struct {
	ID     string
	UserID int64
	Conn   *websocket.Conn
	Send   chan []byte
	Hub    *WSHub
}

type WSHub struct {
	clients      map[*WSClient]bool
	register     chan *WSClient
	unregister   chan *WSClient
	broadcast    chan []byte
	mu           sync.RWMutex
	cleanupTicker *time.Ticker
	done         chan bool
}

func NewWSHub() *WSHub {
	hub := &WSHub{
		clients:      make(map[*WSClient]bool),
		register:     make(chan *WSClient),
		unregister:   make(chan *WSClient),
		broadcast:    make(chan []byte, 256),
		cleanupTicker: time.NewTicker(5 * time.Minute),
		done:         make(chan bool),
	}
	go hub.cleanupRoutine()
	return hub
}

func (h *WSHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("[WS] Client registered: %s (user %d)", client.ID, client.UserID)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)
			}
			h.mu.Unlock()
			log.Printf("[WS] Client unregistered: %s", client.ID)

		case message := <-h.broadcast:
			h.mu.RLock()
			// Create a copy of clients to avoid holding lock
			clients := make([]*WSClient, 0, len(h.clients))
			for client := range h.clients {
				clients = append(clients, client)
			}
			h.mu.RUnlock()

			for _, client := range clients {
				select {
				case client.Send <- message:
					// Success
				default:
					// Client not responding, remove them
					log.Printf("[WS] Client %s send buffer full, unregistering", client.ID)
					go func(c *WSClient) {
						h.unregister <- c
					}(client)
				}
			}
		}
	}
}

func (h *WSHub) BroadcastToUser(userID int64, data []byte) {
	h.mu.RLock()
	// Create a copy of clients with matching userID
	var clients []*WSClient
	for client := range h.clients {
		if client.UserID == userID {
			clients = append(clients, client)
		}
	}
	h.mu.RUnlock()

	for _, client := range clients {
		select {
		case client.Send <- data:
			// Success
		default:
			// Client not responding, unregister in background
			log.Printf("[WS] Client %s send buffer full for user %d, unregistering", client.ID, userID)
			go func(c *WSClient) {
				h.unregister <- c
			}(client)
		}
	}
}

func (h *WSHub) BroadcastAll(data []byte) {
	select {
	case h.broadcast <- data:
	default:
		log.Printf("[WS] Broadcast channel full, dropping message")
	}
}

// Cleanup routine for stale clients
func (h *WSHub) cleanupRoutine() {
	for {
		select {
		case <-h.done:
			h.cleanupTicker.Stop()
			return
		case <-h.cleanupTicker.C:
			h.cleanupStaleClients()
		}
	}
}

// Clean up stale clients
func (h *WSHub) cleanupStaleClients() {
	h.mu.Lock()
	defer h.mu.Unlock()

	var staleClients []*WSClient
	for client := range h.clients {
		if client.Conn == nil {
			staleClients = append(staleClients, client)
		}
	}

	for _, client := range staleClients {
		delete(h.clients, client)
		close(client.Send)
		log.Printf("[WS] Cleaned up stale client: %s", client.ID)
	}
}

// Shutdown the hub gracefully
func (h *WSHub) Shutdown() {
	close(h.done)
	h.mu.Lock()
	for client := range h.clients {
		if client.Conn != nil {
			msg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Server shutting down")
			client.Conn.WriteMessage(websocket.CloseMessage, msg)
			client.Conn.Close()
		}
		close(client.Send)
	}
	h.clients = make(map[*WSClient]bool)
	h.mu.Unlock()
	log.Println("[WS] Hub shutdown complete")
}

func HandleWebSocket(hub *WSHub, engine *Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		userIDStr := c.Query("user_id")
		userID, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			c.JSON(400, gin.H{"error": "invalid user_id"})
			return
		}

		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Printf("[WS] WebSocket upgrade error: %v", err)
			return
		}

		// Set initial deadlines
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

		// Set ping/pong handlers
		conn.SetPingHandler(func(data string) error {
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			return conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(5*time.Second))
		})

		conn.SetPongHandler(func(data string) error {
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			return nil
		})

		// Set close handler
		conn.SetCloseHandler(func(code int, text string) error {
			log.Printf("[WS] Connection closed: user=%d, code=%d, text=%s", userID, code, text)
			return nil
		})

		client := &WSClient{
			ID:     userIDStr + "_" + strconv.FormatInt(time.Now().UnixNano(), 10),
			UserID: userID,
			Conn:   conn,
			Send:   make(chan []byte, 256),
			Hub:    hub,
		}

		hub.register <- client

		// Send initial game state with recovery
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[WS] Panic in sendInitialState: %v", r)
				}
			}()
			sendInitialState(client, engine)
		}()

		go client.writePump()
		go client.readPump(hub, engine)
	}
}

func (c *WSClient) readPump(hub *WSHub, engine *Engine) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[WS] Panic in readPump: %v", r)
		}
		hub.unregister <- c
		c.Conn.Close()
	}()

	// Set read limit and deadline
	c.Conn.SetReadLimit(512 * 1024)
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))

	// Set pong handler
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Set close handler
	c.Conn.SetCloseHandler(func(code int, text string) error {
		log.Printf("[WS] Client %s disconnected: code=%d, text=%s", c.ID, code, text)
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			// Don't log normal disconnections
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseAbnormalClosure,
				websocket.CloseNormalClosure,
				websocket.CloseNoStatusReceived) {
				log.Printf("[WS] Unexpected close error for client %s: %v", c.ID, err)
			} else {
				log.Printf("[WS] Client %s disconnected: %v", c.ID, err)
			}
			break
		}

		// Reset read deadline on each message
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		var req WSRequest
		if err := json.Unmarshal(message, &req); err != nil {
			log.Printf("[WS] Invalid message from client %s: %v", c.ID, err)
			continue
		}

		// Handle message in a goroutine to prevent blocking
		go handleWSMessage(c, engine, req)
	}
}

func (c *WSClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		if r := recover(); r != nil {
			log.Printf("[WS] Panic in writePump: %v", r)
		}
		if c.Conn != nil {
			c.Conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			c.Conn.Close()
		}
	}()

	for {
		select {
		case message, ok := <-c.Send:
			if !ok {
				// Channel closed, send close message
				if c.Conn != nil {
					c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
					c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				}
				return
			}

			if c.Conn == nil {
				return
			}

			// Set write deadline
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

			// Handle write errors gracefully
			if err := c.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				// Don't log broken pipe errors too loudly
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					log.Printf("[WS] Failed to write message to client %s: %v", c.ID, err)
				}
				return
			}

		case <-ticker.C:
			if c.Conn == nil {
				return
			}

			// Set write deadline
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

			// Send ping
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				// Don't log broken pipe errors
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					log.Printf("[WS] Failed to send ping to client %s: %v", c.ID, err)
				}
				return
			}
		}
	}
}

type WSRequest struct {
	Type        string `json:"type"`
	CardNumbers []int  `json:"card_numbers,omitempty"`
	CardNumber  int    `json:"card_number,omitempty"`
	CardID      string `json:"card_id,omitempty"`
}

func handleWSMessage(client *WSClient, engine *Engine, req WSRequest) {
	log.Printf("[WS] Handling message type: %s from user %d", req.Type, client.UserID)

	switch req.Type {
	case "card.select":
		game, cards, err := engine.JoinGame(client.UserID, req.CardNumbers)
		if err != nil {
			sendToClient(client, WSResponse{Type: "error", Message: err.Error()})
			return
		}
		sendToClient(client, WSResponse{
			Type:    "cards.selected",
			GameID:  game.ID.String(),
			Cards:   cards,
			Message: "Cards purchased successfully",
		})

	case "bingo.claim":
		sendToClient(client, WSResponse{Type: "info", Message: "Bingo claim received"})

	case "card.reserve":
		err := engine.ReserveCard(client.UserID, req.CardNumber)
		if err != nil {
			sendToClient(client, WSResponse{
				Type:    "error",
				Message: err.Error(),
			})
			return
		}

	case "card.cancel":
		log.Printf("[WS] User %d cancelling card %d", client.UserID, req.CardNumber)
		err := engine.CancelReservation(client.UserID, req.CardNumber)
		if err != nil {
			sendToClient(client, WSResponse{
				Type:    "error",
				Message: err.Error(),
			})
			return
		}

	case "game.state":
		state, err := engine.GetGameState(client.UserID)
		if err != nil {
			sendToClient(client, WSResponse{Type: "error", Message: err.Error()})
			return
		}
		sendToClient(client, WSResponse{
			Type:  "game.state",
			State: state,
		})

	default:
		log.Printf("[WS] Unknown message type: %s", req.Type)
		sendToClient(client, WSResponse{
			Type:    "error",
			Message: "Unknown message type",
		})
	}
}

type WSResponse struct {
	Type    string      `json:"type"`
	GameID  string      `json:"game_id,omitempty"`
	Cards   interface{} `json:"cards,omitempty"`
	State   interface{} `json:"state,omitempty"`
	Message string      `json:"message,omitempty"`
}

// sendToClient with recovery
func sendToClient(client *WSClient, resp WSResponse) {
	if client == nil {
		return
	}

	data, err := json.Marshal(resp)
	if err != nil {
		log.Printf("[WS] Failed to marshal response: %v", err)
		return
	}

	// Use defer recover to prevent panic on closed channel
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[WS] Recovered from panic sending to client %s: %v", client.ID, r)
		}
	}()

	select {
	case client.Send <- data:
		// Success
	default:
		// Channel is full or closed
		log.Printf("[WS] Client %s send buffer full or channel closed", client.ID)
	}
}

// sendInitialState with recovery
func sendInitialState(client *WSClient, engine *Engine) {
	if client == nil {
		return
	}

	defer func() {
		if r := recover(); r != nil {
			log.Printf("[WS] Panic in sendInitialState for client %s: %v", client.ID, r)
		}
	}()

	state, err := engine.GetGameState(client.UserID)
	if err != nil {
		// No active game
		sendToClient(client, WSResponse{
			Type: "game.state",
			State: map[string]interface{}{
				"status": "idle",
			},
			Message: "Waiting for next game",
		})
		return
	}

	sendToClient(client, WSResponse{
		Type:  "game.state",
		State: state,
	})
}

// SubscribeToRedis subscribes to Redis pub/sub for multi-instance support
func SubscribeToRedis(hub *WSHub, engine *Engine) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Redis] Panic in SubscribeToRedis: %v", r)
		}
	}()

	pubsub := engine.SubscribeEvents()
	if pubsub == nil {
		log.Printf("[Redis] Failed to subscribe to events")
		return
	}
	defer pubsub.Close()

	ch := pubsub.Channel()
	for msg := range ch {
		if msg != nil && len(msg.Payload) > 0 {
			hub.BroadcastAll([]byte(msg.Payload))
		}
	}
}

// GracefulShutdown handles graceful shutdown of the server
func GracefulShutdown(hub *WSHub, engine *Engine) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("🛑 Shutting down server...")

		// Shutdown hub
		if hub != nil {
			hub.Shutdown()
		}

		// Shutdown engine
		if engine != nil {
			engine.Shutdown()
		}

		log.Println("✅ Server shutdown complete")
		os.Exit(0)
	}()
}