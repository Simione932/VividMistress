
go build -o server.exe -buildvcs=false .
.\server -addr :8080 -data ./data -roles ./roles
