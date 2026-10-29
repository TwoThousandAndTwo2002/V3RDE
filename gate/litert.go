//go:build litert

package gate

// The LiteRT C API (google-ai-edge/LiteRT v1.4.0, litert/c/*.h), declared by
// hand for only the calls the gate makes, so no header tree is vendored. The
// library is libLiteRtRuntimeCApi.so from Google's ai-edge-litert 1.4.0
// wheel, pinned by sha256 for each CPU type in the Dockerfile
// (the two wheels were downloaded and hashed 2026-10-29). The struct layouts
// are the ones of litert_layout.h and litert_model.h; element type 1 is
// kTfLiteFloat32 and hardware accelerator 1 is the CPU.

/*
#cgo LDFLAGS: -lLiteRtRuntimeCApi
#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#include <string.h>
#include <stdlib.h>

typedef int LiteRtStatus;
typedef size_t LiteRtParamIndex;
typedef struct LiteRtEnvironmentT* LiteRtEnvironment;
typedef struct LiteRtModelT* LiteRtModel;
typedef struct LiteRtOptionsT* LiteRtOptions;
typedef struct LiteRtOpaqueOptionsT* LiteRtOpaqueOptions;
typedef struct LiteRtCpuOptionsT* LiteRtCpuOptions;
typedef struct LiteRtCompiledModelT* LiteRtCompiledModel;
typedef struct LiteRtSubgraphT* LiteRtSubgraph;
typedef struct LiteRtTensorT* LiteRtTensor;
typedef struct LiteRtTensorBufferT* LiteRtTensorBuffer;
typedef struct LiteRtTensorBufferRequirementsT* LiteRtTensorBufferRequirements;
typedef struct {
  unsigned int rank : 7;
  bool has_strides : 1;
  int32_t dimensions[8];
  uint32_t strides[8];
} LiteRtLayout;
typedef struct {
  int element_type;
  LiteRtLayout layout;
} LiteRtRankedTensorType;

LiteRtStatus LiteRtCreateModelFromFile(const char* filename, LiteRtModel* model);
void LiteRtDestroyModel(LiteRtModel model);
LiteRtStatus LiteRtCreateOptions(LiteRtOptions* options);
void LiteRtDestroyOptions(LiteRtOptions options);
LiteRtStatus LiteRtSetOptionsHardwareAccelerators(LiteRtOptions options, int hw);
LiteRtStatus LiteRtAddOpaqueOptions(LiteRtOptions options, LiteRtOpaqueOptions opaque);
void LiteRtDestroyOpaqueOptions(LiteRtOpaqueOptions options);
LiteRtStatus LiteRtCreateCpuOptions(LiteRtOpaqueOptions* options);
LiteRtStatus LiteRtFindCpuOptions(LiteRtOpaqueOptions opaque, LiteRtCpuOptions* cpu);
LiteRtStatus LiteRtSetCpuOptionsNumThread(LiteRtCpuOptions options, int num_threads);
LiteRtStatus LiteRtCreateEnvironment(int num_options, const void* options, LiteRtEnvironment* env);
void LiteRtDestroyEnvironment(LiteRtEnvironment env);
LiteRtStatus LiteRtCreateCompiledModel(LiteRtEnvironment env, LiteRtModel model,
                                       LiteRtOptions options, LiteRtCompiledModel* cm);
void LiteRtDestroyCompiledModel(LiteRtCompiledModel cm);
LiteRtStatus LiteRtGetModelSubgraph(LiteRtModel model, LiteRtParamIndex i, LiteRtSubgraph* sg);
LiteRtStatus LiteRtGetSubgraphInput(LiteRtSubgraph sg, LiteRtParamIndex i, LiteRtTensor* t);
LiteRtStatus LiteRtGetSubgraphOutput(LiteRtSubgraph sg, LiteRtParamIndex i, LiteRtTensor* t);
LiteRtStatus LiteRtGetRankedTensorType(LiteRtTensor t, LiteRtRankedTensorType* rt);
LiteRtStatus LiteRtGetCompiledModelInputBufferRequirements(LiteRtCompiledModel cm,
    LiteRtParamIndex sig, LiteRtParamIndex i, LiteRtTensorBufferRequirements* req);
LiteRtStatus LiteRtGetCompiledModelOutputBufferRequirements(LiteRtCompiledModel cm,
    LiteRtParamIndex sig, LiteRtParamIndex i, LiteRtTensorBufferRequirements* req);
LiteRtStatus LiteRtGetTensorBufferRequirementsSupportedTensorBufferType(
    LiteRtTensorBufferRequirements req, int idx, int* type);
LiteRtStatus LiteRtGetTensorBufferRequirementsBufferSize(LiteRtTensorBufferRequirements req, size_t* size);
LiteRtStatus LiteRtCreateManagedTensorBuffer(LiteRtEnvironment env, int type,
    const LiteRtRankedTensorType* rt, size_t size, LiteRtTensorBuffer* buf);
void LiteRtDestroyTensorBuffer(LiteRtTensorBuffer buf);
LiteRtStatus LiteRtLockTensorBuffer(LiteRtTensorBuffer buf, void** addr, int mode);
LiteRtStatus LiteRtUnlockTensorBuffer(LiteRtTensorBuffer buf);
LiteRtStatus LiteRtRunCompiledModel(LiteRtCompiledModel cm, LiteRtParamIndex sig,
    size_t n_in, LiteRtTensorBuffer* in, size_t n_out, LiteRtTensorBuffer* out);

static int rt_rank(const LiteRtRankedTensorType* t) { return (int)t->layout.rank; }
static int rt_dim(const LiteRtRankedTensorType* t, int i) { return (int)t->layout.dimensions[i]; }
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

const (
	elemFloat32 = 1 // kTfLiteFloat32
	hwCPU       = 1 // kLiteRtHwAcceleratorCpu
	lockRead    = 0 // kLiteRtTensorBufferLockModeRead
	lockWrite   = 1 // kLiteRtTensorBufferLockModeWrite
)

// LiteRT is one compiled model with its input and output buffers. The
// buffers are used again for each call, so Embed runs one call at a time
// (one interpreter x one thread per replica: more load = more replicas).
type LiteRT struct {
	mu       sync.Mutex
	env      C.LiteRtEnvironment
	model    C.LiteRtModel
	cm       C.LiteRtCompiledModel
	in, out  C.LiteRtTensorBuffer
	inShape  []int
	outShape []int
	inN      int
	outN     int
}

// check turns a LiteRT status into an error that names the step.
func check(s C.LiteRtStatus, step string) error {
	if s != 0 {
		return fmt.Errorf("litert %s: status %d", step, int(s))
	}
	return nil
}

func shapeOf(t *C.LiteRtRankedTensorType) []int {
	s := make([]int, int(C.rt_rank(t)))
	for i := range s {
		s[i] = int(C.rt_dim(t, C.int(i)))
	}
	return s
}

func elements(shape []int) int {
	n := 1
	for _, d := range shape {
		n *= d
	}
	return n
}

// OpenLiteRT loads a model with one float32 input and one float32 output,
// on the CPU with threads threads. A failure closes what was made.
func OpenLiteRT(path string, threads int) (*LiteRT, error) {
	l := &LiteRT{}
	ok := false
	defer func() {
		if !ok {
			l.Close()
		}
	}()
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	// Every C out-parameter is a local variable, copied into l after the
	// call. A pointer into l that reaches C through a variable (as buf in
	// buffer does) is checked as the whole struct l, and l holds Go pointers
	// (the shape slices), so cgo refuses it ("Go pointer
	// to unpinned Go pointer"; https://pkg.go.dev/cmd/cgo#hdr-Passing_pointers:
	// only an &s.f written in the call itself is checked as the field alone).
	// The tiny-model parity test found this.
	var model C.LiteRtModel
	if err := check(C.LiteRtCreateModelFromFile(cpath, &model), "load model"); err != nil {
		return nil, err
	}
	l.model = model
	var opts C.LiteRtOptions
	if err := check(C.LiteRtCreateOptions(&opts), "options"); err != nil {
		return nil, err
	}
	defer C.LiteRtDestroyOptions(opts)
	var opaque C.LiteRtOpaqueOptions
	var cpu C.LiteRtCpuOptions
	var env C.LiteRtEnvironment
	var cm C.LiteRtCompiledModel
	for _, step := range []struct {
		s    func() C.LiteRtStatus
		name string
	}{
		{func() C.LiteRtStatus { return C.LiteRtSetOptionsHardwareAccelerators(opts, hwCPU) }, "cpu accelerator"},
		{func() C.LiteRtStatus { return C.LiteRtCreateCpuOptions(&opaque) }, "cpu options"},
		{func() C.LiteRtStatus { return C.LiteRtFindCpuOptions(opaque, &cpu) }, "find cpu options"},
		{func() C.LiteRtStatus { return C.LiteRtSetCpuOptionsNumThread(cpu, C.int(threads)) }, "threads"},
		{func() C.LiteRtStatus {
			s := C.LiteRtAddOpaqueOptions(opts, opaque)
			if s == 0 {
				opaque = nil // opts owns it now
			}
			return s
		}, "add cpu options"},
		{func() C.LiteRtStatus { return C.LiteRtCreateEnvironment(0, nil, &env) }, "environment"},
		{func() C.LiteRtStatus { l.env = env; return C.LiteRtCreateCompiledModel(env, model, opts, &cm) }, "compile"},
	} {
		if err := check(step.s(), step.name); err != nil {
			if opaque != nil {
				C.LiteRtDestroyOpaqueOptions(opaque) // the X38g review: not freed before
			}
			return nil, err
		}
	}
	l.cm = cm
	var sg C.LiteRtSubgraph
	var tin, tout C.LiteRtTensor
	var rin, rout C.LiteRtRankedTensorType
	for _, step := range []struct {
		s    func() C.LiteRtStatus
		name string
	}{
		{func() C.LiteRtStatus { return C.LiteRtGetModelSubgraph(l.model, 0, &sg) }, "subgraph"},
		{func() C.LiteRtStatus { return C.LiteRtGetSubgraphInput(sg, 0, &tin) }, "input"},
		{func() C.LiteRtStatus { return C.LiteRtGetSubgraphOutput(sg, 0, &tout) }, "output"},
		{func() C.LiteRtStatus { return C.LiteRtGetRankedTensorType(tin, &rin) }, "input type"},
		{func() C.LiteRtStatus { return C.LiteRtGetRankedTensorType(tout, &rout) }, "output type"},
	} {
		if err := check(step.s(), step.name); err != nil {
			return nil, err
		}
	}
	if rin.element_type != elemFloat32 || rout.element_type != elemFloat32 {
		return nil, fmt.Errorf("model is not float32 in/out (%d/%d)", int(rin.element_type), int(rout.element_type))
	}
	l.inShape, l.outShape = shapeOf(&rin), shapeOf(&rout)
	l.inN, l.outN = elements(l.inShape), elements(l.outShape)
	var reqIn, reqOut C.LiteRtTensorBufferRequirements
	if err := check(C.LiteRtGetCompiledModelInputBufferRequirements(l.cm, 0, 0, &reqIn), "input requirements"); err != nil {
		return nil, err
	}
	if err := check(C.LiteRtGetCompiledModelOutputBufferRequirements(l.cm, 0, 0, &reqOut), "output requirements"); err != nil {
		return nil, err
	}
	var bin, bout C.LiteRtTensorBuffer
	if err := l.buffer(reqIn, &rin, &bin, "input"); err != nil {
		return nil, err
	}
	l.in = bin
	if err := l.buffer(reqOut, &rout, &bout, "output"); err != nil {
		return nil, err
	}
	l.out = bout
	ok = true
	return l, nil
}

// buffer makes a managed tensor buffer of the type and size the compiled
// model asks for.
func (l *LiteRT) buffer(req C.LiteRtTensorBufferRequirements, rt *C.LiteRtRankedTensorType, buf *C.LiteRtTensorBuffer, what string) error {
	var typ C.int
	var size C.size_t
	if err := check(C.LiteRtGetTensorBufferRequirementsSupportedTensorBufferType(req, 0, &typ), what+" buffer type"); err != nil {
		return err
	}
	if err := check(C.LiteRtGetTensorBufferRequirementsBufferSize(req, &size), what+" buffer size"); err != nil {
		return err
	}
	return check(C.LiteRtCreateManagedTensorBuffer(l.env, typ, rt, size, buf), what+" buffer")
}

// InputShape is a copy of the input shape.
func (l *LiteRT) InputShape() []int { return append([]int(nil), l.inShape...) }

// OutputShape is a copy of the output shape.
func (l *LiteRT) OutputShape() []int { return append([]int(nil), l.outShape...) }

// Embed runs one input (as many floats as the input shape holds) and returns
// a copy of the output.
func (l *LiteRT) Embed(x []float32) ([]float32, error) {
	if len(x) != l.inN {
		return nil, fmt.Errorf("input has %d floats, model wants %d", len(x), l.inN)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cm == nil {
		return nil, errors.New("litert: closed")
	}
	var p unsafe.Pointer
	if err := check(C.LiteRtLockTensorBuffer(l.in, &p, lockWrite), "lock input"); err != nil {
		return nil, err
	}
	copy(unsafe.Slice((*float32)(p), l.inN), x)
	if err := check(C.LiteRtUnlockTensorBuffer(l.in), "unlock input"); err != nil {
		return nil, err
	}
	in, out := l.in, l.out // C pointers in Go variables: cgo allows &in and &out
	if err := check(C.LiteRtRunCompiledModel(l.cm, 0, 1, &in, 1, &out), "run"); err != nil {
		return nil, err
	}
	if err := check(C.LiteRtLockTensorBuffer(l.out, &p, lockRead), "lock output"); err != nil {
		return nil, err
	}
	y := append([]float32(nil), unsafe.Slice((*float32)(p), l.outN)...)
	if err := check(C.LiteRtUnlockTensorBuffer(l.out), "unlock output"); err != nil {
		return nil, err
	}
	return y, nil
}

// Close frees the buffers, the compiled model, the model and the environment.
func (l *LiteRT) Close() {
	if l == nil {
		return
	}
	l.mu.Lock() // not during an Embed
	defer l.mu.Unlock()
	if l.in != nil {
		C.LiteRtDestroyTensorBuffer(l.in)
		l.in = nil
	}
	if l.out != nil {
		C.LiteRtDestroyTensorBuffer(l.out)
		l.out = nil
	}
	if l.cm != nil {
		C.LiteRtDestroyCompiledModel(l.cm)
		l.cm = nil
	}
	if l.model != nil {
		C.LiteRtDestroyModel(l.model)
		l.model = nil
	}
	if l.env != nil {
		C.LiteRtDestroyEnvironment(l.env)
		l.env = nil
	}
}
