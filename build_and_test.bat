SET PATH=C:\Users\liuyawu\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.25.1.windows-amd64\bin\;D:\soft\mingw\mingw64\bin\;%PATH%
SET GOOS=windows
SET GOARCH=amd64
go build -o ninja-go.exe ./cmd/ninja
cd testdata
del .ninja_deps
del .ninja_log
..\\ninja-go.exe -f simple.ninja