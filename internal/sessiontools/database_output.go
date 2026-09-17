package sessiontools

import (
	"bytes"
	"os"
	"sync"
	"unicode/utf8"
)

const maxDatabaseOutput = 100 * 1024

type boundedDatabaseOutput struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	truncated bool
}

func (o *boundedDatabaseOutput) Write(value []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	written := len(value)
	available := maxDatabaseOutput - o.buffer.Len()
	if available <= 0 {
		o.truncated = true
		return written, nil
	}
	if len(value) > available {
		value = value[:available]
		o.truncated = true
	}
	_, _ = o.buffer.Write(value)
	return written, nil
}

func (o *boundedDatabaseOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	value := append([]byte(nil), o.buffer.Bytes()...)
	for len(value) > 0 && !utf8.Valid(value) {
		value = value[:len(value)-1]
	}
	if !o.truncated {
		return string(value)
	}
	const marker = "\n[output truncated at 100 KiB]"
	available := maxDatabaseOutput - len(marker)
	if len(value) > available {
		value = value[:available]
		for len(value) > 0 && !utf8.Valid(value) {
			value = value[:len(value)-1]
		}
	}
	return string(value) + marker
}

func (o *boundedDatabaseOutput) Truncated() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.truncated
}

func writeBackgroundSecret(name, value string) (string, error) {
	file, err := os.CreateTemp("", "koko-agent-tool-"+name+"-*.pem")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err = file.Chmod(0600); err == nil {
		_, err = file.WriteString(value)
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func removeBackgroundSecrets(paths ...string) {
	for _, path := range paths {
		if path != "" {
			_ = os.Remove(path)
		}
	}
}
