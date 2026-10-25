package room

import (
	"iter"
	"sync"
	"time"

	"github.com/giongto35/cloud-game/v3/pkg/worker/caged/app"
	"github.com/giongto35/cloud-game/v3/pkg/worker/media"
)

type MediaPipe interface {
	// Destroy frees all allocated resources.
	Destroy()
	// Init initializes the pipe: allocates needed resources.
	Init() error
	// Reinit initializes video and audio pipes with the new settings.
	Reinit() error
	// ProcessAudio pushes 16bit PCM audio frames into the encoder.
	ProcessAudio([]byte, func([]byte, time.Duration))
	// ProcessVideo pushes a video frame into the encoder.
	ProcessVideo(media.Video, func([]byte, time.Duration))
}

type SessionManager[T Session] interface {
	Add(T) bool
	Empty() bool
	Find(string) T
	Remove(T)
	// Reset used for proper cleanup of the resources if needed.
	Reset()
	Values() iter.Seq[T]
}

type Session interface {
	Disconnect()
	SendAudio([]byte, time.Duration)
	SendVideo([]byte, time.Duration)
	SendData([]byte)
}

type SessionKey string

func (s SessionKey) String() string { return string(s) }
func (s SessionKey) Id() string     { return s.String() }

type Room[T Session] struct {
	app   app.App
	id    string
	media MediaPipe
	users SessionManager[T]

	peers   []T
	peersMu sync.RWMutex

	closed      bool
	HandleClose func()
}

func NewRoom[T Session](id string, app app.App, um SessionManager[T], media MediaPipe) *Room[T] {
	return &Room[T]{id: id, app: app, users: um, media: media}
}

func (r *Room[T]) InitMedia() {
	r.app.SetAudioCb(func(a app.Audio) {
		r.media.ProcessAudio(a.Data, r.sendAudio)
	})
	r.app.SetVideoCb(func(v app.Video) {
		r.media.ProcessVideo(media.Video{
			Frame:    media.RawFrame{Data: v.Frame.Data, W: v.Frame.W, H: v.Frame.H, Stride: v.Frame.Stride},
			Duration: v.Duration,
		}, r.sendVideo)
	})
}

func (r *Room[T]) sendAudio(data []byte, dur time.Duration) {
	r.peersMu.RLock()
	peers := r.peers
	r.peersMu.RUnlock()
	for _, u := range peers {
		u.SendAudio(data, dur)
	}
}

func (r *Room[T]) sendVideo(data []byte, dur time.Duration) {
	r.peersMu.RLock()
	peers := r.peers
	r.peersMu.RUnlock()
	for _, u := range peers {
		u.SendVideo(data, dur)
	}
}

func (r *Room[T]) App() app.App         { return r.app }
func (r *Room[T]) Id() string           { return r.id }
func (r *Room[T]) SetApp(app app.App)   { r.app = app }
func (r *Room[T]) SetMedia(m MediaPipe) { r.media = m }
func (r *Room[T]) StartApp()            { r.app.Start() }
func (r *Room[T]) Send(data []byte) {
	r.peersMu.RLock()
	peers := r.peers
	r.peersMu.RUnlock()
	for _, u := range peers {
		u.SendData(data)
	}
}

// SyncPeers rebuilds the cached peer slice from the session manager.
// Must be called after any add/remove/reset of sessions.
func (r *Room[T]) SyncPeers() {
	if r.users == nil {
		return
	}
	r.peersMu.Lock()
	r.peers = r.peers[:0]
	for u := range r.users.Values() {
		r.peers = append(r.peers, u)
	}
	r.peersMu.Unlock()
}

func (r *Room[T]) Close() {
	if r == nil || r.closed {
		return
	}
	r.closed = true

	if r.app != nil {
		r.app.Close()
	}
	if r.media != nil {
		r.media.Destroy()
	}
	if r.HandleClose != nil {
		r.HandleClose()
	}
}

// Router tracks and routes freshly connected users to an app room.
// Rooms and users has 1-to-n relationship.
type Router[T Session] struct {
	room  *Room[T]
	users SessionManager[T]
	mu    sync.Mutex
}

func (r *Router[T]) FindRoom(id string) *Room[T] {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.room != nil && r.room.Id() == id {
		return r.room
	}
	return nil
}

func (r *Router[T]) AddUser(user T) {
	r.mu.Lock()
	r.users.Add(user)
	r.mu.Unlock()
	if r.room != nil {
		r.room.SyncPeers()
	}
}
func (r *Router[T]) FindUser(uid string) T    { return r.users.Find(uid) }
func (r *Router[T]) Users() SessionManager[T] { return r.users }
func (r *Router[T]) Room() *Room[T]           { r.mu.Lock(); defer r.mu.Unlock(); return r.room }
func (r *Router[T]) SetRoom(room *Room[T]) {
	r.mu.Lock()
	r.room = room
	r.mu.Unlock()
	if room != nil {
		room.SyncPeers()
	}
}
func (r *Router[T]) HasRoom() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.room != nil }
func (r *Router[T]) Close()        { r.mu.Lock(); r.close(); r.mu.Unlock() }
func (r *Router[T]) Reset() {
	r.mu.Lock()
	r.close()
	for u := range r.users.Values() {
		u.Disconnect()
	}
	r.users.Reset()
	if r.room != nil {
		r.room.SyncPeers()
	}
	r.mu.Unlock()
}
func (r *Router[T]) Remove(user T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users.Remove(user)
	if r.room != nil {
		r.room.SyncPeers()
	}
	if r.users.Empty() {
		r.close()
	}
}
func (r *Router[T]) close() {
	if r.room != nil {
		r.room.Close()
		r.room = nil
	}
}

type AppSession struct {
	Session
	uid SessionKey
}

func (p AppSession) Id() SessionKey { return p.uid }

type GameSession struct {
	AppSession
	Index int // track user Index (i.e. player 1,2,3,4 select), -1 for a cabinet watcher
	order int // cabinet join order, 0 when not in a cabinet
}

func NewGameSession(id string, s Session) *GameSession {
	return &GameSession{AppSession: AppSession{uid: SessionKey(id), Session: s}}
}

// Explicitly forward the embedded Session methods to work around a Go 1.27 linker bug:
// promoted wrappers mis-link when called from generics.
func (g GameSession) SendAudio(data []byte, dur time.Duration) { g.Session.SendAudio(data, dur) }
func (g GameSession) SendVideo(data []byte, dur time.Duration) { g.Session.SendVideo(data, dur) }
func (g GameSession) SendData(data []byte)                     { g.Session.SendData(data) }
func (g GameSession) Disconnect()                              { g.Session.Disconnect() }
