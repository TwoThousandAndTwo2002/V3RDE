module github.com/TwoThousandAndTwo2002/Icarus/post

go 1.24.0

require (
	github.com/TwoThousandAndTwo2002/Icarus/inference v0.0.0
	github.com/TwoThousandAndTwo2002/Icarus/listings v0.0.0
	github.com/TwoThousandAndTwo2002/Icarus/server v0.0.0
	github.com/go-audio/audio v1.0.0
	github.com/go-audio/wav v1.1.0
)

require (
	github.com/go-audio/riff v1.0.0 // indirect
	golang.org/x/image v0.36.0 // indirect
)

replace github.com/TwoThousandAndTwo2002/Icarus/server => ../server

replace github.com/TwoThousandAndTwo2002/Icarus/listings => ../listings

replace github.com/TwoThousandAndTwo2002/Icarus/inference => ../inference
