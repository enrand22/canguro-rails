# Third-party notices

This repository vendors one third-party asset so that applications built with it
work offline and are not affected by a CDN outage or a strict Content Security
Policy.

## htmx 2.0.4

- File: `web/static/js/htmx.min.js`
- Project: https://htmx.org — https://github.com/bigskysoftware/htmx
- License: **Zero-Clause BSD (0BSD)**

Text of the license, as published by the project:

```
Zero-Clause BSD
=============

Permission to use, copy, modify, and/or distribute this software for
any purpose with or without fee is hereby granted.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL
WARRANTIES WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES
OF MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE
FOR ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY
DAMAGES WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN
AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT
OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
```

Everything else in this repository is our own code, under the MIT license (see
`LICENSE`), except the Go dependencies declared in `go.mod`, which keep their own
licenses.
