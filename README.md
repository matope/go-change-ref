# go-change-ref

## What is this?

`go-change-ref` change references to specific Go symbols (Type, Function, Variable...) in your project to another.

This tool will help you with refactoring your project when you want to move a definition to another package.

## How to use.

`github.com/matope/go-change-ref/example/pkg1/pkg2.go`

```go
package pkg1

import "github.com/matope/go-change-ref/example/pkg2"

func main() {
 var t1 = pkg2.T1
 fmt.Println(t1)
}
```

```
$ go install github.com/matope/go-change-ref
$ go-change-ref -from github.com/matope/go-change-ref/example/pkg2.T1 -to github.com/matope/go-change-ref/example/pkg3.T2 ./...
```

```go
package pkg1

import "github.com/matope/go-change-ref/example/pkg3"

func main() {
 var t1 = pkg3.T2
 fmt.Println(t1)
}
```
