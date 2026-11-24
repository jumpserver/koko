package core

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jumpserver/koko/pkg/asciinema"
)

// Replay contains the SQL and the masked result content supplied by the
// execution owner. Command audit retains its separate status summary.
type replayStore struct {
	dir      string
	mu       sync.Mutex
	transfer *replayTransfer // accessed only by the serialized outbox flush
}

var replaySessionID = regexp.MustCompile(`^[a-zA-Z0-9-]{1,64}$`)

func newReplayStore(dir string) (*replayStore, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("invalid_replay_directory")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	return &replayStore{dir: dir}, nil
}

func (r *replayStore) start(g *Grant) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !replaySessionID.MatchString(g.ID) {
		return errors.New("invalid_replay_session_id")
	}
	f, err := os.OpenFile(filepath.Join(r.dir, g.ID+".cast"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	writer := asciinema.NewWriter(f, asciinema.WithTimestamp(g.created), asciinema.WithWidth(160),
		asciinema.WithTitle("Chen SQL session"), asciinema.WithEnvShell("sql"))
	err = writer.WriteHeader()
	return errors.Join(err, f.Sync(), f.Close(), syncDirectory(r.dir))
}

func (r *replayStore) record(g *Grant, input, output string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !replaySessionID.MatchString(g.ID) {
		return errors.New("invalid_replay_session_id")
	}
	f, err := os.OpenFile(filepath.Join(r.dir, g.ID+".cast"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	elapsed := at.Sub(g.created).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	text := fmt.Sprintf("[Input]\r\n%s\r\n[Output]\r\n%s\r\n", input, output)
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
	err = asciinema.NewWriter(f).WriteStdout(elapsed, []byte(text))
	return errors.Join(err, f.Sync(), f.Close())
}

func (r *replayStore) finish(id string) (*replayRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !replaySessionID.MatchString(id) {
		return nil, errors.New("invalid_replay_session_id")
	}
	path := filepath.Join(r.dir, id+".cast")
	gzPath := path + ".gz"
	if _, err := os.Stat(gzPath); err == nil {
		return &replayRequest{SessionID: id}, nil
	}
	src, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} // Failed before replay initialization.
	if err != nil {
		return nil, err
	}
	defer src.Close()
	dst, err := os.CreateTemp(r.dir, ".compress-")
	if err != nil {
		return nil, err
	}
	defer os.Remove(dst.Name())
	w := gzip.NewWriter(dst)
	_, copyErr := io.Copy(w, src)
	err = errors.Join(copyErr, w.Close(), dst.Sync(), dst.Close())
	if err != nil {
		return nil, err
	}
	if err := os.Rename(dst.Name(), gzPath); err != nil {
		return nil, err
	}
	if err := syncDirectory(r.dir); err != nil {
		return nil, err
	}
	// Keep both files until Core acknowledges the uploaded replay.
	return &replayRequest{SessionID: id}, nil
}

// Recover locally durable casts after an unclean process exit. Existing outbox
// entries own their files already; do not enqueue a second upload for them.
func (p *CoreProvider) recoverReplays() error {
	pending := map[string]bool{}
	entries, err := os.ReadDir(p.spool.dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		e, err := readEnvelope(filepath.Join(p.spool.dir, entry.Name()))
		if err != nil {
			return err
		}
		if e.Replay != nil {
			pending[e.Replay.SessionID] = true
		}
	}
	entries, err = os.ReadDir(p.replays.dir)
	if err != nil {
		return err
	}
	if err := p.spool.claimReplays(entries); err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (!strings.HasSuffix(name, ".cast") && !strings.HasSuffix(name, ".cast.gz")) {
			continue
		}
		id := strings.TrimSuffix(strings.TrimSuffix(name, ".gz"), ".cast")
		if pending[id] {
			continue
		}
		request, err := p.replays.finish(id)
		if err != nil {
			return err
		}
		if request == nil {
			continue
		}
		if err := p.spool.put(envelope{Replay: request, Finish: &finishRequest{SessionID: id, DateEnd: time.Now().Unix()}}); err != nil {
			return err
		}
		pending[id] = true
	}
	return nil
}

func (r *replayStore) remove(id string) error {
	if !replaySessionID.MatchString(id) {
		return errors.New("invalid_replay_session_id")
	}
	for _, suffix := range []string{".cast.gz", ".cast"} {
		if err := os.Remove(filepath.Join(r.dir, id+suffix)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return syncDirectory(r.dir)
}
