// A modified version of Tengo's script compilation and run implementation.
// https://github.com/d5/tengo

package tengox

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/d5/tengo/v2"
	"github.com/d5/tengo/v2/parser"
)

const reservedVar = "$out"

// Script is used to call callable Tengo objects from Go.
// Methods are derived from Tengo's Script and Compiled types. Unlike Tengo,
// Compile and Run methods are called as CompileRun().
type Script struct {
	mu        sync.RWMutex
	bytecode  *tengo.Bytecode
	modules   *tengo.ModuleMap
	indexes   map[string]int
	variables map[string]*tengo.Variable
	globals   []tengo.Object
	src       []byte
	maxAllocs int64
	outIdx    int
	inited    bool
}

// NewScript creates a Script object to call *tengo.CompiledFunction in scripts.
func NewScript(src []byte) *Script {
	return &Script{
		src:       src,
		variables: make(map[string]*tengo.Variable),
		maxAllocs: -1,
	}
}

// CompileRun compiles the source script to bytecode and run it.
func (s *Script) CompileRun() error {
	return s.compile(nil)
}

// CompileRunContext compiles the source script to bytecode and run it.
func (s *Script) CompileRunContext(ctx context.Context) error {
	return s.compile(ctx)
}

func (s *Script) compile(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inited {
		// ignore silently if already inited
		return nil
	}

	var err error
	var symbolTable *tengo.SymbolTable
	symbolTable, s.globals, err = s.prepCompile()
	if err != nil {
		return err
	}

	fileSet := parser.NewFileSet()
	srcFile := fileSet.AddFile("(main)", -1, len(s.src))
	p := parser.NewParser(srcFile, s.src, nil)
	file, err := p.ParseFile()
	if err != nil {
		return err
	}

	out := symbolTable.Define(reservedVar)
	s.globals[out.Index] = tengo.UndefinedValue

	cc := tengo.NewCompiler(srcFile, symbolTable, nil, s.modules, nil)
	if err := cc.Compile(file); err != nil {
		return err
	}

	// reduce globals size
	s.globals = s.globals[:symbolTable.MaxSymbols()+1]

	// global symbol names to indexes
	globalIndexes := make(map[string]int, len(s.globals))
	for _, name := range symbolTable.Names() {
		symbol, _, _ := symbolTable.Resolve(name)
		if symbol.Scope == tengo.ScopeGlobal {
			globalIndexes[name] = symbol.Index
		}
	}

	// run VM once and fill globals
	if ctx == nil {
		vm := tengo.NewVM(cc.Bytecode(), s.globals, s.maxAllocs)
		if err := vm.Run(); err != nil {
			return err
		}
	} else {
		vm := tengo.NewVM(cc.Bytecode(), s.globals, s.maxAllocs)
		if err := s.runContext(ctx, vm); err != nil {
			return err
		}
	}

	bc := cc.Bytecode()
	bc.RemoveDuplicates()
	s.bytecode = bc
	s.outIdx = out.Index
	s.indexes = globalIndexes
	s.inited = true
	return nil
}

func (s *Script) prepCompile() (
	symbolTable *tengo.SymbolTable,
	globals []tengo.Object,
	err error,
) {
	var names []string
	for name := range s.variables {
		names = append(names, name)
	}

	symbolTable = tengo.NewSymbolTable()

	globals = make([]tengo.Object, tengo.GlobalsSize)

	for idx, name := range names {
		symbol := symbolTable.Define(name)
		if symbol.Index != idx {
			panic(fmt.Errorf("wrong symbol index: %d != %d",
				idx, symbol.Index))
		}
		globals[symbol.Index] = s.variables[name].Object()
	}
	return
}

// Clone creates a new copy of Script. Cloned copies are safe for concurrent
// use. Note: It panics if Script.Compile() is not called before Clone().
// Cloned Script occupies less memory..
func (s *Script) Clone() *Script {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.inited {
		panic("not compiled")
	}

	clone := &Script{
		indexes:   s.indexes,
		bytecode:  s.bytecode,
		globals:   make([]tengo.Object, len(s.globals)),
		maxAllocs: s.maxAllocs,
		outIdx:    s.outIdx,
		inited:    true,
	}
	// copy global objects
	for idx, g := range s.globals {
		if g != nil {
			clone.globals[idx] = g
		}
	}
	return clone
}

// CallByName calls tengo.CompiledFunction with given arguments and returns result.
func (s *Script) CallByName(fn string, args ...interface{}) (interface{}, error) {
	return s.CallByNameContext(nil, fn, args...)
}

// CallByNameContext calls tengo.CompiledFunction with given arguments and returns result.
func (s *Script) CallByNameContext(ctx context.Context,
	fn string, args ...interface{}) (interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx, ok := s.indexes[fn]
	if !ok {
		return nil, fmt.Errorf("'%s' not found", fn)
	}
	cfn := s.globals[idx]
	if cfn == nil {
		return nil, errors.New("callable expected, got nil")
	}
	if !cfn.CanCall() {
		return nil, errors.New("not a callable")
	}

	return s.call(ctx, cfn, args...)
}

// Call calls given callable tengo.Object and returns result.
// args must be convertible to supported Tengo types.
func (s *Script) Call(fn tengo.Object,
	args ...interface{}) (interface{}, error) {
	return s.CallContext(nil, fn, args...)
}

// CallContext calls given callable tengo.Object and returns result.
// args must be convertible to supported Tengo types.
func (s *Script) CallContext(ctx context.Context, fn tengo.Object,
	args ...interface{}) (interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if fn == nil {
		return nil, errors.New("callable expected, got nil")
	}
	if !fn.CanCall() {
		return nil, errors.New("not a callable")
	}

	return s.call(ctx, fn, args...)
}

func (s *Script) call(ctx context.Context, cfn tengo.Object,
	args ...interface{}) (interface{}, error) {
	if !s.inited {
		return nil, errors.New("not compiled")
	}
	targs := make([]tengo.Object, 0, len(args))
	for i := range args {
		v, err := tengo.FromInterface(args[i])
		if err != nil {
			return nil, err
		}
		targs = append(targs, v)
	}

	v, err := s.callCompiled(ctx, cfn, targs...)
	if err != nil {
		return nil, err
	}
	return tengo.ToInterface(v), nil
}

func (s *Script) callCompiled(ctx context.Context, fn tengo.Object,
	args ...tengo.Object) (tengo.Object, error) {

	constsOffset := len(s.bytecode.Constants)

	// Load fn
	inst := tengo.MakeInstruction(parser.OpConstant, constsOffset)

	// Load args
	for i := range args {
		inst = append(inst,
			tengo.MakeInstruction(parser.OpConstant, constsOffset+i+1)...)
	}

	// Call, set value to a global, stop
	inst = append(inst, tengo.MakeInstruction(parser.OpCall, len(args))...)
	inst = append(inst, tengo.MakeInstruction(parser.OpSetGlobal, s.outIdx)...)
	inst = append(inst, tengo.MakeInstruction(parser.OpSuspend)...)

	s.bytecode.Constants = append(s.bytecode.Constants, fn)
	s.bytecode.Constants = append(s.bytecode.Constants, args...)

	// orig := s.bytecode.MainFunction
	s.bytecode.MainFunction = &tengo.CompiledFunction{
		Instructions: inst,
	}

	var err error
	if ctx == nil {
		vm := tengo.NewVM(s.bytecode, s.globals, s.maxAllocs)
		err = vm.Run()
	} else {
		vm := tengo.NewVM(s.bytecode, s.globals, s.maxAllocs)
		err = s.runContext(ctx, vm)
	}

	// go back to normal if required
	// s.bytecode.MainFunction = orig
	s.bytecode.Constants = s.bytecode.Constants[:constsOffset]

	// get symbol using index and return it
	return s.globals[s.outIdx], err
}

func (s *Script) runContext(ctx context.Context, vm *tengo.VM) (err error) {
	errch := make(chan error)
	go func() {
		errch <- vm.Run()
	}()
	select {
	case err = <-errch:
	case <-ctx.Done():
		vm.Abort()
		<-errch
		err = ctx.Err()
	}
	if err != nil {
		return
	}
	return
}

// Add adds a new variable or updates an existing variable to the script.
// Add variable before compilation.
func (s *Script) Add(name string, value interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inited {
		// makes no sense to add variable after compilation
		return errors.New("already compiled")
	}
	if name == reservedVar {
		return errors.New("variable name must be different")
	}

	v, err := tengo.NewVariable(name, value)
	if err != nil {
		return err
	}
	s.variables[name] = v
	return nil
}

// Remove removes (undefines) an existing variable for the script. It returns
// false if the variable name is not defined.
// Remove variable before compilation.
func (s *Script) Remove(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inited {
		// makes no sense to remove after compilation
		return false
	}
	if name == reservedVar {
		return false
	}

	if _, ok := s.variables[name]; !ok {
		return false
	}
	delete(s.variables, name)
	return true
}

// SetImports sets import modules. Set imports before compilation.
func (s *Script) SetImports(modules *tengo.ModuleMap) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inited {
		// silently ignore if already compiled
		return
	}
	s.modules = modules
}

// SetMaxAllocs sets the maximum number of objects allocations during the run
// time. Compiled script will return tengo.ErrObjectAllocLimit error if it
// exceeds this limit.
// Set the maximum number of objects allocations before compilation.
func (s *Script) SetMaxAllocs(n int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.maxAllocs = n
}

// Get returns a variable identified by the name. Get variables after execution.
func (s *Script) Get(name string) *tengo.Variable {
	s.mu.RLock()
	defer s.mu.RUnlock()

	value := tengo.UndefinedValue
	if idx, ok := s.indexes[name]; ok {
		value = s.globals[idx]
		if value == nil {
			value = tengo.UndefinedValue
		}
	}

	v, err := tengo.NewVariable(name, value)
	if err != nil {
		// This should never happen, since the value will already be a tengo object
		// tengo's (*Script).Get avoids this because it constructs the object directly.
		panic(fmt.Errorf("unable to create new variable from global value, type was %T", value))
	}

	return v
}

// GetAll returns all the variables that are defined by the compiled script.
func (s *Script) GetAll() []*tengo.Variable {
	s.mu.RLock()
	defer s.mu.RUnlock()

	vars := make([]*tengo.Variable, 0, len(s.indexes))
	for name, idx := range s.indexes {
		value := s.globals[idx]
		if value == nil {
			value = tengo.UndefinedValue
		}
		tv, err := tengo.NewVariable(name, value)
		if err != nil {
			// This should never happen, since the value will already be a tengo object
			panic(fmt.Errorf("unable to create new variable from global value, type was %T", value))
		}
		vars = append(vars, tv)
	}
	return vars
}

// Set replaces the value of a global variable identified by the name. An error
// will be returned if the name was not defined during compilation.
func (s *Script) Set(name string, value interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	obj, err := tengo.FromInterface(value)
	if err != nil {
		return err
	}

	idx, ok := s.indexes[name]
	if !ok {
		return fmt.Errorf("'%s' is not defined", name)
	}

	s.globals[idx] = obj
	return nil
}

// IsDefined returns true if the variable name is defined (has value)
// after the execution.
func (s *Script) IsDefined(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	idx, ok := s.indexes[name]
	if !ok {
		return false
	}
	v := s.globals[idx]
	if v == nil {
		return false
	}
	return v != tengo.UndefinedValue
}

// MakeCallback makes a tengo.Object callable from Go.
func (s *Script) MakeCallback(fn tengo.Object) *Callback {
	return &Callback{
		script: s,
		fn:     fn,
	}
}

// Callback is a wrapper to call tengo functions from Go with Call() and
// CallContext() methods.
type Callback struct {
	script *Script
	fn     tengo.Object
}

// Call calls tengo.Object and returns result.
func (c *Callback) Call(args ...interface{}) (interface{}, error) {
	return c.script.Call(c.fn, args...)
}

// CallContext calls tengo.Object and returns result.
func (c *Callback) CallContext(ctx context.Context,
	args ...interface{}) (interface{}, error) {
	return c.script.CallContext(ctx, c.fn, args...)
}
