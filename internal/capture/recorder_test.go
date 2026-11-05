package capture

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
)

// heldRecordFile makes the writer wait until the test releases its first write.
type heldRecordFile struct {
	*os.File
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (f *heldRecordFile) Write(p []byte) (int, error) {
	f.once.Do(func() {
		close(f.entered)
		<-f.release
	})
	return f.File.Write(p)
}

func TestRecorder_OverflowProducesVisibleGapWithoutBlocking(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, recordsFilename)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		held := &heldRecordFile{File: file, entered: make(chan struct{}), release: make(chan struct{})}
		release := sync.OnceFunc(func() { close(held.release) })
		recorder := newRecorder(held, dir, path, "session-overflow", "overflow", 1)
		defer func() { release(); _ = recorder.Close(CaptureComplete, 0) }()
		if err := recorder.Record(Record{Kind: RecordSessionStart, Label: "overflow"}); err != nil {
			t.Fatal(err)
		}
		synctest.Wait() // the small start record has left the queue
		large := Record{Kind: RecordProviderResponseBody, BodyBase64: strings.Repeat("eA==", 20_000), BodyBytes: 32 * 1024}
		if err := recorder.Record(large); err != nil {
			t.Fatal(err)
		}
		<-held.entered // the large record forces the buffered writer into held.Write
		if err := recorder.Record(Record{Kind: RecordProviderResponseBody, BodyBase64: "eA==", BodyBytes: 32 * 1024}); err != nil {
			t.Fatal(err)
		}
		type result struct {
			gaps int
			err  error
		}
		done := make(chan result, 1)
		go func() {
			var got result
			for range 20 {
				err := recorder.Record(Record{Kind: RecordProviderResponseBody, BodyBase64: "eA==", BodyBytes: 32 * 1024})
				if errors.Is(err, ErrCaptureGap) {
					got.gaps++
				} else if err != nil {
					got.err = err
					break
				}
			}
			done <- got
		}()
		synctest.Wait()
		var got result
		select {
		case got = <-done:
		default:
			release()
			t.Error("overflowing Record calls blocked while the writer was held")
			got = <-done
		}
		if got.err != nil {
			t.Fatalf("Record() error = %v", got.err)
		}
		if got.gaps == 0 {
			t.Error("overflow produced no ErrCaptureGap")
		}
		release()
		if err := recorder.Close(CaptureComplete, 0); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		records, err := ReadRecords(recorder.Path())
		if err != nil {
			t.Fatalf("ReadRecords() error = %v", err)
		}
		gap := firstRecord(t, records, RecordCaptureGap)
		if gap.DroppedRecords == 0 || gap.DroppedBytes == 0 || gap.GapStartSeq == 0 || gap.GapEndSeq < gap.GapStartSeq {
			t.Fatalf("gap record = %+v, want explicit lost range and counts", gap)
		}
		end := records[len(records)-1]
		if end.Kind != RecordSessionEnd || end.CaptureStatus != CapturePartial {
			t.Fatalf("session end = %+v, want gap to force partial status", end)
		}
	})
}

func TestRecorder_WriteFailureIsReturnedOnClose(t *testing.T) {
	t.Parallel()

	writer := &failingRecordFile{writeErr: errors.New("disk full")}
	recorder := newRecorder(writer, t.TempDir(), "provider.jsonl", "session-write-failure", "failure", 8)
	if err := recorder.Record(Record{Kind: RecordSessionStart}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if err := recorder.Close(CaptureComplete, 0); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("Close() error = %v, want visible disk failure", err)
	}
}

type failingRecordFile struct {
	writeErr error
}

func (f *failingRecordFile) Write(_ []byte) (int, error) { return 0, f.writeErr }
func (f *failingRecordFile) Sync() error                 { return nil }
func (f *failingRecordFile) Close() error                { return nil }

var _ io.WriteCloser = (*failingRecordFile)(nil)

func TestRecorder_PrivateFilesAndLifecycle(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "captures")
	recorder, err := NewRecorder(RecorderConfig{
		RootDir:   root,
		SessionID: "session-private",
		Label:     "weather/classic",
	})
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	if err := recorder.Close(CapturePartial, 7); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	dirInfo, err := os.Stat(recorder.SessionDir())
	if err != nil {
		t.Fatalf("stat session dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("session dir mode = %#o, want 0700", got)
	}
	fileInfo, err := os.Stat(recorder.Path())
	if err != nil {
		t.Fatalf("stat records file: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("records file mode = %#o, want 0600", got)
	}

	records, err := ReadRecords(recorder.Path())
	if err != nil {
		t.Fatalf("ReadRecords() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("record count = %d, want session start + end", len(records))
	}
	if records[0].Kind != RecordSessionStart || records[0].Label != "weather/classic" {
		t.Fatalf("session start = %+v", records[0])
	}
	if records[1].Kind != RecordSessionEnd || records[1].CaptureStatus != CapturePartial || records[1].ChildExitCode != 7 {
		t.Fatalf("session end = %+v", records[1])
	}
	if records[0].Seq != 1 || records[1].Seq != 2 {
		t.Fatalf("sequence = [%d %d], want [1 2]", records[0].Seq, records[1].Seq)
	}
}
