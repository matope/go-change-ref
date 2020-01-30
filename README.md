# go-change-ref

## What is this?

`go-change-ref` changes all references to specific Go symbols (Type, Function, Variable...) in your project to another. This tool will be especially useful for changing package organization during refactoring.

## Install

```
$ go install github.com/matope/go-change-ref
```

## How to use

After you moved a type definition pkg2.T2 to pkg3.T3, you can change all references to pkg2.T2 to pkg3.T3 with `go-change-ref`.


### Before

```go
package main

import (
	"fmt"

	"github.com/matope/go-change-ref/example/pkg2"
)

func main() {
	var t2 pkg2.T2
	fmt.Println(t2)
}
```

### Run
```
$ go-change-ref -w \
    -from github.com/matope/go-change-ref/example/pkg2.T2 \
    -to   github.com/matope/go-change-ref/example/pkg3.T3 \
    ./...
```

### After

```go
package main

import (
	"fmt"

	"github.com/matope/go-change-ref/example/pkg3" // UPDATED.
)

func main() {
	var t2 pkg3.T3 // UPDATED.
	fmt.Println(t2)
}
```

### flags

```
  -from string
        from symbol. importpath.name
  -to string
        to symbol. importpath.name.
  -to-pkg-name string
        package name used when the package name conflits with another imported package
  -w    overwrite .go code
```
