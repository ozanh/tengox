# tengox

> I archived this repo in favor of a new script language [uGO](https://github.com/ozanh/ugo) for Go.

Experimental features for [Tengo Scripting Language](https://github.com/d5/tengo)

A special thanks to [Tom Gascoigne](https://github.com/tgascoigne) for his contribution.

tengox is created to call callable Tengo functions from Go in an easier way.
Please see tests, example files and [godoc](https://godoc.org/github.com/ozanh/tengox).

You should pin tengox to a git tag (if any) in your `go.mod` file to use the stable code.

```go
func ExampleCompiled_CallByName() {
	module := `add := func(a, b, c) { return a + b + c; }`
	script := tengox.NewScript([]byte(module))
	// script is compiled and run
	compl, err := script.CompileRun() // CompileRunContext() is available
	if err != nil {
		panic(err)
	}
	// CallByNameContext() is available
	v, err := compl.CallByName("add", 1, 2, 3) // 1+2+3
	if err != nil {
		panic(err)
	}
	fmt.Println(v)
	//Output: 6

	// you can clone your compiled code like this
	// clone := compl.Clone()
	// cloned code run in a different VM
}
```
