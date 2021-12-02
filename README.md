# yterm

`yterm` is the smallest serial reader.

## install

```
$ go install github.com/sago35/yterm@latest
```

Alternatively, you can download the binary from the following URL.  
However, the macOS version of the binary is not available at this time.  
(see: https://github.com/bugst/go-serial/issues/114)  

* https://github.com/sago35/yterm/releases

## Usage

```
# If there is only one port, it will connect without any argument.
$ yterm
$ tinygo flash --target wioterminal --size short && yterm

# If --port is specified, it will connect to that port.
$ yterm --port COM8
$ tinygo flash --target wioterminal --size short && yterm --port COM8

# When the list subcommand is specified, port information will be displayed.
$ yterm list
/dev/ttyACM0 2886 802f xiao
/dev/ttyACM1 2886 802d wioterminal
```

## Notice

This project uses [goreleaser](https://goreleaser.com/) for release

## LICENSE

MIT

## Author

sago35 - <sago35@gmail.com>
