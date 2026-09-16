// Package dock sert le panneau que l'utilisateur ajoute dans OBS.
//
// C'est l'agent lui-même qui héberge la page, sur 127.0.0.1. Aucun plugin à
// compiler, aucune extension à installer : OBS sait afficher un dock de
// navigateur depuis toujours. Si l'agent n'est pas lancé, le dock reste vide,
// ce qui est exactement le bon message d'erreur.
package dock

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gilforge/stream-team/internal/pipeline"
)

type Server struct {
	engine *pipeline.Engine
	http   *http.Server

	mu             sync.Mutex
	publishWanted  bool
	lastMessage    string
	unconfigured   []string
	appliedVersion int
	appliedAuthor  string
}

func New(e *pipeline.Engine, rep *pipeline.Report) *Server {
	s := &Server{engine: e}
	if rep != nil {
		s.unconfigured = rep.Unconfigured
		s.appliedVersion = rep.Version
		s.appliedAuthor = rep.Author
	}
	return s
}

// Start écoute sur la boucle locale uniquement : rien de ce que fait l'agent
// n'a de raison d'être joignable depuis le réseau.
func (s *Server) Start(port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handlePage)
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/publish-intent", s.handleIntent)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("port %d déjà occupé — une autre instance de l'agent tourne-t-elle ? %w", port, err)
	}
	s.http = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go s.http.Serve(ln)
	return nil
}

func (s *Server) Stop() {
	if s.http == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s.http.Shutdown(ctx)
}

// PublishRequested indique si l'utilisateur a demandé la publication depuis le
// dock. Lu une fois OBS fermé.
func (s *Server) PublishRequested() (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.publishWanted, s.lastMessage
}

type state struct {
	Version      int      `json:"version"`
	Author       string   `json:"author"`
	CanPublish   bool     `json:"can_publish"`
	HasChanges   bool     `json:"has_changes"`
	WillPublish  bool     `json:"will_publish"`
	Message      string   `json:"message"`
	Unconfigured []string `json:"unconfigured"`
	Error        string   `json:"error,omitempty"`
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	st := state{
		Version:      s.appliedVersion,
		Author:       s.appliedAuthor,
		CanPublish:   s.engine.Pub != nil,
		WillPublish:  s.publishWanted,
		Message:      s.lastMessage,
		Unconfigured: s.unconfigured,
	}
	s.mu.Unlock()

	changed, err := s.engine.HasLocalChanges()
	if err != nil {
		st.Error = err.Error()
	}
	st.HasChanges = changed

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(st)
}

func (s *Server) handleIntent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "méthode non autorisée", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Publish bool   `json:"publish"`
		Message string `json:"message"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	s.mu.Lock()
	s.publishWanted = body.Publish && s.engine.Pub != nil
	s.lastMessage = body.Message
	s.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(pageHTML))
}
