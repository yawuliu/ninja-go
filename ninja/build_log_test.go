package main

import (
	"os"
	"testing"
)

// mockBuildLogUser implements BuildLogUser for testing
type mockBuildLogUser struct {
	deadPaths map[string]bool
}

func (m *mockBuildLogUser) IsPathDead(path string) bool {
	return m.deadPaths[path]
}

func TestBuildLog_WriteRead(t *testing.T) {
	state := NewState()
	fs := newMockFSIntegration()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})
	var perr string
	parser.ParseTest(
		"rule cat\n  command = cat $in > $out\n"+
			"build out: cat mid\n"+
			"build mid: cat in\n", &perr)

	logFile := "build_log_test_WR.tmp"
	defer os.Remove(logFile)

	log1 := NewBuildLog(logFile)
	var err string
	user := &mockBuildLogUser{}
	if !log1.OpenForWrite(logFile, user, &err) {
		t.Fatalf("OpenForWrite failed: %s", err)
	}
	log1.RecordCommand(state.edges_[0], 15, 18, 18)
	log1.RecordCommand(state.edges_[1], 20, 25, 25)
	log1.Close()

	log2 := NewBuildLog(logFile)
	status := log2.Load(logFile, &err)
	if status != LOAD_SUCCESS {
		t.Fatalf("Load failed: %s (status=%d)", err, status)
	}

	if len(log1.Entries()) != 2 {
		t.Errorf("log1: expected 2 entries, got %d", len(log1.Entries()))
	}
	if len(log2.Entries()) != 2 {
		t.Errorf("log2: expected 2 entries, got %d", len(log2.Entries()))
	}
	e1 := log1.LookupByOutput("out")
	if e1 == nil {
		t.Errorf("log1: 'out' not found")
	}
	e2 := log2.LookupByOutput("out")
	if e2 == nil {
		t.Errorf("log2: 'out' not found")
	}
	if e1 != nil && e2 != nil {
		if e1.start_time != e2.start_time {
			t.Errorf("start_time mismatch: %d vs %d", e1.start_time, e2.start_time)
		}
		if e1.mtime != e2.mtime {
			t.Errorf("mtime mismatch: %d vs %d", e1.mtime, e2.mtime)
		}
	}
}

func TestBuildLog_FirstWriteAddsSignature(t *testing.T) {
	logFile := "build_log_test_FWAS.tmp"
	defer os.Remove(logFile)

	log := NewBuildLog(logFile)
	var err string
	user := &mockBuildLogUser{}
	if !log.OpenForWrite(logFile, user, &err) {
		t.Fatalf("OpenForWrite failed: %s", err)
	}
	log.Close()

	content, readErr := ReadFile(logFile)
	if readErr != nil {
		t.Fatalf("ReadFile failed: %v", readErr)
	}
	expectedSig := "# ninja log v7\n"
	if content != expectedSig {
		t.Errorf("expected signature %q, got %q", expectedSig, content)
	}

	// Opening again should not add a second signature
	if !log.OpenForWrite(logFile, user, &err) {
		t.Fatalf("2nd OpenForWrite failed: %s", err)
	}
	log.Close()

	content2, _ := ReadFile(logFile)
	if content2 != expectedSig {
		t.Errorf("after 2nd open: expected %q, got %q", expectedSig, content2)
	}
}

func TestBuildLog_DoubleEntry(t *testing.T) {
	logFile := "build_log_test_DE.tmp"
	defer os.Remove(logFile)

	f, err := os.Create(logFile)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	f.WriteString("# ninja log v7\n")
	f.WriteString("0\t1\t2\tout\t12345\n")
	f.WriteString("0\t1\t2\tout\t67890\n")
	f.Close()

	log := NewBuildLog(logFile)
	var loadErr string
	status := log.Load(logFile, &loadErr)
	if status != LOAD_SUCCESS {
		t.Fatalf("Load failed: %s", loadErr)
	}
	e := log.LookupByOutput("out")
	if e == nil {
		t.Fatalf("'out' not found")
	}
	// Last entry should win for duplicate outputs
	expectedHash := uint64(67890)
	if e.command_hash != expectedHash {
		t.Errorf("expected command_hash %d, got %d", expectedHash, e.command_hash)
	}
}

func TestBuildLog_ObsoleteOldVersion(t *testing.T) {
	logFile := "build_log_test_OOV.tmp"
	defer os.Remove(logFile)

	f, err := os.Create(logFile)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	f.WriteString("# ninja log v3\n")
	f.WriteString("123 456 0 out command\n")
	f.Close()

	log := NewBuildLog(logFile)
	var loadErr string
	status := log.Load(logFile, &loadErr)
	// Old version should result in NOT_FOUND (file removed)
	if status != LOAD_NOT_FOUND {
		t.Errorf("expected LOAD_NOT_FOUND for old version, got status=%d", status)
	}
}

func TestBuildLog_MultiTargetEdge(t *testing.T) {
	state := NewState()
	fs := newMockFSIntegration()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})
	var perr string
	parser.ParseTest("rule cat\n  command = cat $in > $out\nbuild out out.d: cat\n", &perr)

	log := NewBuildLog("")
	log.RecordCommand(state.edges_[0], 21, 22, 22)

	if len(log.Entries()) != 2 {
		t.Errorf("expected 2 entries, got %d", len(log.Entries()))
	}
	e1 := log.LookupByOutput("out")
	e2 := log.LookupByOutput("out.d")
	if e1 == nil || e2 == nil {
		t.Fatalf("entries not found: %v, %v", e1, e2)
	}
	if e1.start_time != 21 || e2.start_time != 21 {
		t.Errorf("expected start_time=21, got %d/%d", e1.start_time, e2.start_time)
	}
	if e1.end_time != 22 || e2.end_time != 22 {
		t.Errorf("expected end_time=22, got %d/%d", e1.end_time, e2.end_time)
	}
}

func TestBuildLog_Recompact(t *testing.T) {
	state := NewState()
	fs := newMockFSIntegration()
	parser := NewManifestParser(state, fs, ManifestParserOptions{})
	var perr string
	parser.ParseTest(
		"rule cat\n  command = cat $in > $out\n"+
			"build out: cat in\n"+
			"build out2: cat in\n", &perr)

	logFile := "build_log_test_RC.tmp"
	defer os.Remove(logFile)

	log1 := NewBuildLog(logFile)
	user := &mockBuildLogUser{}
	var err string
	if !log1.OpenForWrite(logFile, user, &err) {
		t.Fatalf("OpenForWrite failed: %s", err)
	}
	// Record same edge many times to trigger recompaction
	for i := 0; i < 200; i++ {
		log1.RecordCommand(state.edges_[0], 15, 18+i, 18+int64(i))
	}
	log1.RecordCommand(state.edges_[1], 21, 22, 22)
	log1.Close()

	// Load and check
	log2 := NewBuildLog(logFile)
	status := log2.Load(logFile, &err)
	if status != LOAD_SUCCESS {
		t.Fatalf("Load failed: %s", err)
	}
	if len(log2.Entries()) != 2 {
		t.Errorf("before recompact: expected 2 entries, got %d", len(log2.Entries()))
	}
	if log2.LookupByOutput("out") == nil || log2.LookupByOutput("out2") == nil {
		t.Errorf("entries missing before recompact")
	}

	// Force recompaction
	recompactUser := &mockBuildLogUser{deadPaths: map[string]bool{"out2": true}}
	if !log2.OpenForWrite(logFile, recompactUser, &err) {
		t.Fatalf("OpenForWrite (recompact) failed: %s", err)
	}
	log2.Close()

	// Check that "out2" was removed
	log3 := NewBuildLog(logFile)
	status = log3.Load(logFile, &err)
	if status != LOAD_SUCCESS {
		t.Fatalf("3rd Load failed: %s", err)
	}
	if len(log3.Entries()) != 1 {
		t.Errorf("after recompact: expected 1 entry, got %d", len(log3.Entries()))
	}
	if log3.LookupByOutput("out") == nil {
		t.Errorf("'out' should still exist")
	}
	if log3.LookupByOutput("out2") != nil {
		t.Errorf("'out2' should have been removed")
	}
}

func TestBuildLog_Restat(t *testing.T) {
	logFile := "build_log_test_RST.tmp"
	defer os.Remove(logFile)

	// Create a log file
	f, err := os.Create(logFile)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	f.WriteString("# ninja log v7\n")
	f.WriteString("1\t2\t3\tout\tcommand\n")
	f.Close()

	log := NewBuildLog(logFile)
	var loadErr string
	status := log.Load(logFile, &loadErr)
	if status != LOAD_SUCCESS {
		t.Fatalf("Load failed: %s", loadErr)
	}
	e := log.LookupByOutput("out")
	if e == nil || e.mtime != 3 {
		t.Fatalf("expected mtime=3, got %v", e)
	}

	// Restat with non-matching filter (output "out2") - shouldn't change
	testDisk := &mockFSIntegration{
		files:  map[string]string{"out": ""},
		mtimes: map[string]int64{"out": 4},
	}
	if !log.Restat(logFile, testDisk, []string{"out2"}, &loadErr) {
		t.Fatalf("Restat failed: %s", loadErr)
	}

	e = log.LookupByOutput("out")
	if e == nil || e.mtime != 3 {
		t.Errorf("after filtered restat: expected mtime=3, got %v", e)
	}

	// Restat without filter - should update
	if !log.Restat(logFile, testDisk, nil, &loadErr) {
		t.Fatalf("Restat (no filter) failed: %s", loadErr)
	}

	e = log.LookupByOutput("out")
	if e == nil || e.mtime != 4 {
		t.Errorf("after unfiltered restat: expected mtime=4, got %v", e)
	}
}

func TestBuildLog_DuplicateVersionHeader(t *testing.T) {
	logFile := "build_log_test_DVH.tmp"
	defer os.Remove(logFile)

	f, err := os.Create(logFile)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	f.WriteString("# ninja log v7\n")
	f.WriteString("123\t456\t456\tout\t12345\n")
	f.WriteString("# ninja log v7\n")
	f.WriteString("456\t789\t789\tout2\t67890\n")
	f.Close()

	log := NewBuildLog(logFile)
	var loadErr string
	status := log.Load(logFile, &loadErr)
	if status != LOAD_SUCCESS {
		t.Fatalf("Load failed: %s", loadErr)
	}

	e1 := log.LookupByOutput("out")
	if e1 == nil {
		t.Fatalf("'out' not found")
	}
	if e1.start_time != 123 || e1.end_time != 456 || e1.mtime != 456 {
		t.Errorf("entry 'out': got start=%d end=%d mtime=%d", e1.start_time, e1.end_time, e1.mtime)
	}

	e2 := log.LookupByOutput("out2")
	if e2 == nil {
		t.Fatalf("'out2' not found")
	}
	if e2.start_time != 456 || e2.end_time != 789 || e2.mtime != 789 {
		t.Errorf("entry 'out2': got start=%d end=%d mtime=%d", e2.start_time, e2.end_time, e2.mtime)
	}
}
