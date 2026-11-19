# Third-party notices

net-merge is MIT licensed (see `LICENSE`). It links the following libraries, all under permissive
licenses (license texts are in each project's repository and in the Go module cache):

| Component | License | Use |
|---|---|---|
| [tun2socks](https://github.com/xjasonlyu/tun2socks) | MIT | TUN device + userland TCP/IP stack glue |
| [gVisor](https://github.com/google/gvisor) (netstack) | Apache-2.0 | userland TCP/IP stack |
| [wireguard-go](https://git.zx2c4.com/wireguard-go) / [wintun Go bindings](https://git.zx2c4.com/wintun-go) | MIT | Windows TUN device access |
| [fyne.io/systray](https://github.com/fyne-io/systray) | Apache-2.0 | system tray icon |
| [vishvananda/netlink](https://github.com/vishvananda/netlink) | Apache-2.0 | Linux TUN addresses and routes, default-route discovery |
| golang.org/x/sys, golang.org/x/net, golang.org/x/time, golang.org/x/crypto | BSD-3-Clause | Windows and Linux system APIs, support code |
| Other transitive modules (see `go.mod` / `go.sum`) | MIT / Apache-2.0 / BSD | |

## Wintun

The installer and portable zip include `wintun.dll`, the prebuilt, signed driver from
<https://www.wintun.net/>, unmodified, under the Prebuilt Binaries License. Copyright (C) 2018-2021
WireGuard LLC. The license text is shipped next to it as `wintun-LICENSE.txt`. The DLL is not stored
in this repository: `scripts/fetch-wintun.ps1` downloads it and verifies its SHA-256 and its
WireGuard LLC code signature.

## Walk (v0.0.0-20210112085537-c389da54e794)

Copyright and license for https://github.com/lxn/walk:

```
Copyright (c) 2010 The Walk Authors. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions
are met:
1. Redistributions of source code must retain the above copyright
   notice, this list of conditions and the following disclaimer.
2. Redistributions in binary form must reproduce the above copyright
   notice, this list of conditions and the following disclaimer in the
   documentation and/or other materials provided with the distribution.
3. The names of the authors may not be used to endorse or promote products
   derived from this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE AUTHORS ``AS IS'' AND ANY EXPRESS OR
IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES
OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED.
IN NO EVENT SHALL THE AUTHORS BE LIABLE FOR ANY DIRECT, INDIRECT,
INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT
NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF
THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

## Win (v0.0.0-20210218163916-a377121e959e)

Copyright and license for https://github.com/lxn/win:

```
Copyright (c) 2010 The win Authors. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions
are met:
1. Redistributions of source code must retain the above copyright
   notice, this list of conditions and the following disclaimer.
2. Redistributions in binary form must reproduce the above copyright
   notice, this list of conditions and the following disclaimer in the
   documentation and/or other materials provided with the distribution.
3. The names of the authors may not be used to endorse or promote products
   derived from this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE AUTHORS ``AS IS'' AND ANY EXPRESS OR
IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES
OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED.
IN NO EVENT SHALL THE AUTHORS BE LIABLE FOR ANY DIRECT, INDIRECT,
INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT
NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF
THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```
