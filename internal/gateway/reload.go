package gateway

import (
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"

	"github.com/trustgate/trustgate/internal/config"
	"github.com/trustgate/trustgate/internal/policy"
)

// WatchConfig starts watching the config file for changes and hot-reloads
// runtime-safe settings (workforce, policies, logging level).
// Settings that require restart (detectors, backend, listen, audit) are logged
// but not applied until the next restart.
func (s *Server) WatchConfig(path string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to create config watcher")
		return
	}

	go s.watchLoop(watcher, path)

	if err := watcher.Add(path); err != nil {
		s.logger.Error().Err(err).Str("path", path).Msg("failed to watch config file")
		return
	}

	// Also watch the directory (some editors replace the file via rename)
	dir := path[:len(path)-len("/"+baseName(path))]
	if dir != "" {
		_ = watcher.Add(dir)
	}

	s.logger.Info().Str("path", path).Msg("config file watcher started")
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}

// debounce timer to avoid multiple reloads from editor save patterns
var (
	reloadMu    sync.Mutex
	reloadTimer *time.Timer
)

func (s *Server) watchLoop(watcher *fsnotify.Watcher, path string) {
	defer watcher.Close()

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Only react to write/create/rename events on the config file
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}

			// Debounce: wait 200ms for editor to finish writing
			reloadMu.Lock()
			if reloadTimer != nil {
				reloadTimer.Stop()
			}
			reloadTimer = time.AfterFunc(200*time.Millisecond, func() {
				s.reloadConfig(path)
			})
			reloadMu.Unlock()

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			s.logger.Error().Err(err).Msg("config watcher error")
		}
	}
}

func (s *Server) reloadConfig(path string) {
	newCfg, err := config.Load(path)
	if err != nil {
		s.logger.Error().Err(err).Msg("config reload: failed to parse")
		return
	}

	changed := false

	// 1. Workforce settings (target_sites, enabled, lockout_seconds)
	s.cfg.Workforce = newCfg.Workforce
	changed = true

	// 2. Policies reload
	if newCfg.Policy.Source == "local" && newCfg.Policy.File != "" {
		policies, err := policy.LoadPolicies(newCfg.Policy.File)
		if err != nil {
			s.logger.Warn().Err(err).Msg("config reload: failed to reload policies")
		} else {
			s.evaluator.UpdatePolicies(policies)
			s.logger.Info().Int("count", len(policies)).Msg("config reload: policies updated")
		}
	}

	// 3. Logging level
	if newCfg.Logging.Level != "" {
		if level, err := zerolog.ParseLevel(newCfg.Logging.Level); err == nil {
			zerolog.SetGlobalLevel(level)
			s.logger.Info().Str("level", newCfg.Logging.Level).Msg("config reload: log level updated")
		}
	}

	if changed {
		s.logger.Info().Msg("config reloaded successfully")
	}
}
