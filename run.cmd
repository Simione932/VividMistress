
go build -o server.exe .
.\server -addr :8080 -data ./data -roles ./roles -model gemma-4-12b
