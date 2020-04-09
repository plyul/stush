# SSH & Telnet URL scheme handler for Linux (freedesktop.org compliant)

![Travis CI build status of master](https://api.travis-ci.org/plyul/stush.svg?branch=master)

## Why?

To be able to run system SSH/Telnet client in a terminal by clicking
on a link like `ssh://...` or `telnet://...` in a browser.

## Portability
Tested on Ubuntu Linux Eoan only.

But I belive ```stush``` will work on any system which adheres to XDG Specification.
If this is not the case, feel free to submit an issue.

## Examples
### SSH example
Link: `ssh://sshtest@localhost:2022/?4&o=StrictHostKeyChecking=no&o=UserKnownHostsFile=/dev/null`

Executed command:
`/usr/bin/ssh -p 2022 -4 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null sshtest@localhost`

### Telnet example
Link: `telnet://teltest@localhost:2023/?4&S=0`

Executed command:
`/usr/bin/telnet -l teltest -4 -S 0 localhost 2023`

## Installing

Download executable and run:
```
./stush --install
```

## Uninstalling

Just run:
```
~/.local/bin/stush --remove
```
