"use strict";

let MaxTimeout = 30 * 1000

function message(id, type, data) {
    return JSON.stringify({
        id,
        type,
        data,
    });
}

function handleError(reason) {
    console.log(reason);
}

function decodeToStr(octets) {
    if (typeof TextEncoder == "function") {
        return new TextDecoder("utf-8").decode(new Uint8Array(octets))
    }
    return decodeURIComponent(escape(String.fromCharCode.apply(null, octets)));
}

let DISABLE_RZ_SZ = false; // 监控页面将忽略上传下载 rz、sz

function initTerminal(elementId) {
    if (window.innerHeight === 0) {
        setTimeout(() => initTerminal(elementId), 500)
        return
    }
    let urlParams = new URLSearchParams(window.location.search.slice(1));
    let scheme = document.location.protocol === "https:" ? "wss" : "ws";
    let port = document.location.port ? ":" + document.location.port : "";
    let baseWsUrl = scheme + "://" + document.location.hostname + port;
    let pingInterval;
    let resizeTimer;
    let term;
    let lastSendTime;
    let lastReceiveTime;
    let initialed;
    let ws;
    let terminalId = "";
    let termSelection = "";
    let wsURL = baseWsUrl + '/koko/ws/terminal/?' + urlParams.toString();
    switch (urlParams.get("type")) {
        case 'token':
            wsURL = baseWsUrl + "/koko/ws/token/?" + urlParams.toString();
            break
        case 'shareroom':
            DISABLE_RZ_SZ = true
            break
        default:
    }
    ws = new WebSocket(wsURL, ["JMS-KOKO"]);
    term = createTerminalById(elementId)
    window.term = term;
    window.addEventListener('jmsFocus', evt => {
        term.focus()
        term.scrollToBottom()
    })
    var zsentry;
    // patch send_block_files 能够显示上传进度
    Zmodem.Browser.send_block_files = function (session, files, options) {
        return send_block_files(session, files, options);
    };
    zsentry = new Zmodem.Sentry({
        to_terminal: function (octets) {
            if (!zsentry.get_confirmed_session()) {
                term.write(decodeToStr(octets));
            }
        },
        sender: function (octets) {
            return ws.send(new Uint8Array(octets));
        },
        on_retract: function () {
            console.log('zmodem Retract')
        },
        on_detect: function (detection) {
            var promise;
            let file_input_el;
            var zsession = detection.confirm();
            term.write("\r\n")
            if (zsession.type === "send") {
                // 动态创建 input 标签，否则选择相同的文件，不会触发 onchang 事件
                file_input_el = document.createElement("input");
                file_input_el.type = "file";
                file_input_el.style.display = "none";//隐藏
                document.body.appendChild(file_input_el);
                document.body.onfocus = function () {
                    document.body.onfocus = null;
                    setTimeout(function () {
                        // 如果未选择任何文件，则代表取消上传。主动取消
                        if (file_input_el.files.length === 0) {
                            console.log("Cancel file clicked")
                            if (!zsession.aborted()) {
                                zsession.abort()
                            }
                        }
                    }, 1000);
                }
                promise = _handle_send_session(file_input_el, zsession);
            } else {
                promise = _handle_receive_session(zsession);
            }
            promise.catch(console.error.bind(console)).then(() => {
                console.log("zmodem Detect promise finished")
            }).finally(() => {
                if (file_input_el != null) {
                    document.body.removeChild(file_input_el);
                }
            })

        }
    });

    function resizeTerminal() {
        // 延迟调整窗口大小
        if (resizeTimer != null) {
            clearTimeout(resizeTimer);
        }
        resizeTimer = setTimeout(function () {
            const termRef = document.getElementById('terminal')
            termRef.style.height = (window.innerHeight - 16) + 'px';
            term.fit();
            term.focus();
            let cols = term.cols;
            let rows = term.rows;
            if (initialed == null || ws == null) {
                return
            }
            ws.send(message(terminalId, 'TERMINAL_RESIZE',
                JSON.stringify({cols, rows})));
        }, 500)

    }

    function dispatch(term, data) {
        if (data === undefined) {
            return
        }
        let msg = JSON.parse(data)
        switch (msg.type) {
            case 'CONNECT':
                terminalId = msg.id;
                term.fit();
                let cols = term.cols;
                let rows = term.rows;
                ws.send(message(terminalId, 'TERMINAL_INIT',
                    JSON.stringify({cols, rows})));
                initialed = true;
                resizeTerminal();
                break
            case "CLOSE":
                term.writeln("Receive Connection closed");
                fireEvent(new Event("CLOSE", {}))
                break
            case "PING":
                break
            default:
                console.log(data)
        }
    }

    window.SendTerminalData = function (data) {
        if (ws.readyState === WebSocket.CLOSING ||
            ws.readyState === WebSocket.CLOSED) {
            return
        }
        ws.send(message(terminalId, 'TERMINAL_DATA', data));
    }

    window.addEventListener('resize', resizeTerminal);

    let quickPaste = getQuickPaste();
    let terminalContext = document.getElementById(elementId);
    terminalContext.addEventListener('contextmenu', function ($event) {
        if ($event.ctrlKey || quickPaste !== '1') {
            return;
        }
        if (navigator.clipboard && navigator.clipboard.readText) {
            navigator.clipboard.readText().then((text) => {
                ws.send(message(terminalId, 'TERMINAL_DATA', text))
            })
            $event.preventDefault();
        } else if (termSelection !== "") {
            ws.send(message(terminalId, 'TERMINAL_DATA', termSelection))
            $event.preventDefault();
        }
    })

    term.on('data', data => {
        if (initialed === null || ws === null) {
            return
        }
        lastSendTime = new Date();
        ws.send(message(terminalId, 'TERMINAL_DATA', data));
    });

    term.on('selection', function () {
        document.execCommand('copy');
        // this ==> term object
        termSelection = this.getSelection().trim();
    });
    ws.binaryType = "arraybuffer";
    ws.onopen = () => {
        if (pingInterval !== null) {
            clearInterval(pingInterval);
        }
        lastReceiveTime = new Date();
        pingInterval = setInterval(function () {
            if (ws.readyState === WebSocket.CLOSING ||
                ws.readyState === WebSocket.CLOSED) {
                clearInterval(pingInterval)
                return
            }
            let currentDate = new Date();
            if ((lastReceiveTime - currentDate) > MaxTimeout) {
                console.log("more than 30s do not receive data")
            }
            let pingTimeout = (currentDate - lastSendTime) - MaxTimeout
            if (pingTimeout < 0) {
                return;
            }
            ws.send(message(terminalId, 'PING', ""));
        }, 25 * 1000);
    }
    ws.onerror = (e) => {
        term.writeln("Connection error");
        fireEvent(new Event("CLOSE", {}))
        handleError(e)
    }
    ws.onclose = (e) => {
        term.writeln("Connection closed");
        fireEvent(new Event("CLOSE", {}))
        handleError(e)
    }
    ws.onmessage = (e) => {
        lastReceiveTime = new Date();
        if (typeof e.data === 'object') {
            zsentry.consume(e.data);
        } else {
            dispatch(term, e.data);
        }
    }
}

function createTerminalById(elementId) {
    let fontSize = getFontSize();
    const termRef = document.getElementById('terminal')
    termRef.style.height = (window.innerHeight - 16) + 'px';
    fit.apply(Terminal);
    const ua = navigator.userAgent.toLowerCase();
    let lineHeight = 1;
    if (ua.indexOf('windows') !== -1) {
        lineHeight = 1.2;
    }
    let term = new Terminal({
        fontFamily: 'monaco, Consolas, "Lucida Console", monospace',
        lineHeight: lineHeight,
        fontSize: fontSize,
        rightClickSelectsWord: true,
        theme: {
            background: '#1f1b1b'
        }
    });
    term.open(document.getElementById(elementId));
    term.focus();
    term.attachCustomKeyEventHandler(function (e) {
        if (e.ctrlKey && e.key === 'c' && term.hasSelection()) {
            return false;
        }
        return !(e.ctrlKey && e.key === 'v');
    });

    return term
}

function fireEvent(e) {
    window.dispatchEvent(e)
}

function getFontSize() {
    let fontSize = 14
    // localStorage.getItem default null
    let localSettings = localStorage.getItem('LunaSetting')
    if (localSettings !== null) {
        let settings = JSON.parse(localSettings)
        fontSize = settings['fontSize']
    }
    if (!fontSize || fontSize < 5 || fontSize > 50) {
        fontSize = 13;
    }
    return fontSize
}

function getQuickPaste() {
    let quickPaste = "0"
    let localSettings = localStorage.getItem('LunaSetting')
    if (localSettings !== null) {
        let settings = JSON.parse(localSettings)
        quickPaste = settings['quickPaste']
    }
    return quickPaste
}

function _handle_receive_session(zsession) {
    zsession.on("offer", function (xfer) {
        if (!_validate_transfer_file_size(xfer)) {
            let detail = xfer.get_details()
            let msg = alert_message(detail.name, detail.size, 'download')
            alert(msg)
            xfer.skip();
            return
        }

        function on_form_submit() {
            var FILE_BUFFER = [];
            xfer.on("input", (payload) => {
                _update_progress(xfer, 'download');
                if (DISABLE_RZ_SZ) {
                    console.log("监控状态，忽略rz sz 下载文件")
                    return
                }
                FILE_BUFFER.push(new Uint8Array(payload));
            });
            xfer.accept().then(
                () => {
                    if (DISABLE_RZ_SZ) {
                        console.log("监控状态，忽略rz sz 下载文件")
                        return
                    }
                    _save_to_disk(xfer, FILE_BUFFER);
                },
                console.error.bind(console)
            );
        }

        on_form_submit();
    });

    var promise = new Promise((res) => {
        zsession.on("session_end", () => {
            window.term.write('\r\n')
            res();
            console.log("finished ")
        });
    });

    zsession.start();

    return promise;
}

function alert_message(name, size, action = 'upload') {
    let lang = getCookieByName("django_language");
    let msg_array = [name, "大小", bytesHuman(size), "超过最大传输大小", bytesHuman(MAX_TRANSFER_SIZE)];
    let action_name = action;
    if (lang.startsWith("en")) {
        msg_array = [name, "size", bytesHuman(size), "exceed max transfer size", bytesHuman(MAX_TRANSFER_SIZE)];
    } else {
        switch (action) {
            case "upload":
                action_name = "上传"
                break
            default:
                action_name = "下载"
        }
    }
    msg_array.unshift(action_name)
    return msg_array.join(" ")
}

function getCookieByName(name) {
    var cookies = document.cookie.split("; ")
    for (var i = 0; i < cookies.length; i++) {
        var arr = cookies[i].split("=");
        if (arr[0] === name) {
            return arr[1];
        }
    }
    return "";
}

function _handle_send_session(file_el, zsession) {
    let promise = new Promise((res) => {
        file_el.onchange = function (e) {
            console.log("file input on change", file_el.files)
            let files_obj = file_el.files;
            for (let i = 0; i < files_obj.length; i++) {
                if (files_obj[i].size > MAX_TRANSFER_SIZE) {

                    alert(alert_message(files_obj[i].name, files_obj[i].size))
                    zsession.abort();
                    return
                }
            }
            Zmodem.Browser.send_block_files(zsession, files_obj,
                {
                    on_offer_response(obj, xfer) {
                        if (xfer) {
                            console.log("on_offer_response ", xfer)
                            _update_progress(xfer);
                        }
                    },
                    on_progress(obj, xfer, piece) {
                        _update_progress(xfer);
                    },
                    on_file_complete(obj) {
                        console.log("COMPLETE", obj);
                    },
                }
            ).then(
                zsession.close.bind(zsession),
                console.error.bind(console)
            ).then(() => {
                res();
                window.term.write("\r\n")
            }).catch(err => {
                console.log(err)
            });
        };
        if (DISABLE_RZ_SZ) {
            zsession.abort();
            console.log("监控状态，忽略rz sz 上传文件")
            return
        }
        file_el.click();
    });

    return promise;
}

let MAX_TRANSFER_SIZE = 1024 * 1024 * 500 // 默认最大上传下载500M
// let MAX_TRANSFER_SIZE = 1024 * 1024 * 5 // 测试 上传下载最大size 5M

function _validate_transfer_file_size(xfer) {
    let detail = xfer.get_details();
    return detail.size < MAX_TRANSFER_SIZE
}


function _save_to_disk(xfer, buffer) {
    return Zmodem.Browser.save_to_disk(buffer, xfer.get_details().name);
}

function _update_progress(xfer, action = 'upload') {
    let detail = xfer.get_details();
    let name = detail.name;
    let total = detail.size;
    let offset = xfer.get_offset();
    var percent;
    if (total === 0 || total === offset) {
        percent = 100
    } else {
        percent = Math.round(offset / total * 100);
    }
    let msg = action + ' ' + name + ": " + bytesHuman(total) + " " + percent + "%"
    window.term.write("\r" + msg);
}

function bytesHuman(bytes, precision) {
    if (!/^([-+])?|(\.\d+)(\d+(\.\d+)?|(\d+\.)|Infinity)$/.test(bytes)) {
        return '-'
    }
    if (bytes === 0) return '0';
    if (typeof precision === 'undefined') precision = 1;
    const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB', 'BB'];
    const num = Math.floor(Math.log(bytes) / Math.log(1024));
    const value = (bytes / Math.pow(1024, Math.floor(num))).toFixed(precision);
    return `${value} ${units[num]}`
}


function _check_aborted(session) {
    if (session.aborted()) {
        throw new Zmodem.Error("aborted");
    }
}

// copy from https://github.com/leffss/gowebssh/blob/master/example/html/zmodem/zmodem.devel.js#L922
function send_block_files(session, files, options) {
    if (!options) options = {};

    //Populate the batch in reverse order to simplify sending
    //the remaining files/bytes components.
    var batch = [];
    var total_size = 0;
    for (var f = files.length - 1; f >= 0; f--) {
        var fobj = files[f];
        total_size += fobj.size;
        batch[f] = {
            obj: fobj,
            name: fobj.name,
            size: fobj.size,
            mtime: new Date(fobj.lastModified),
            files_remaining: files.length - f,
            bytes_remaining: total_size,
        };
    }

    var file_idx = 0;

    function promise_callback() {
        var cur_b = batch[file_idx];

        if (!cur_b) {
            return Promise.resolve(); //batch done!
        }

        file_idx++;

        return session.send_offer(cur_b).then(function after_send_offer(xfer) {
            if (options.on_offer_response) {
                options.on_offer_response(cur_b.obj, xfer);
            }

            if (xfer === undefined) {
                return promise_callback();   //skipped
            }

            return new Promise(function (res) {
                var block = 1024 * 1024;
                var fileSize = cur_b.size;
                var fileLoaded = 0;
                var reader = new FileReader();
                reader.onerror = function reader_onerror(e) {
                    console.error('file read error', e);
                    throw ('File read error: ' + e);
                };

                function readBlob() {
                    var blob;
                    if (cur_b.obj.slice) {
                        blob = cur_b.obj.slice(fileLoaded, fileLoaded + block + 1);
                    } else if (cur_b.obj.mozSlice) {
                        blob = cur_b.obj.mozSlice(fileLoaded, fileLoaded + block + 1);
                    } else if (cur_b.obj.webkitSlice) {
                        blob = cur_b.obj.webkitSlice(fileLoaded, fileLoaded + block + 1);
                    } else {
                        blob = cur_b.obj;
                    }
                    reader.readAsArrayBuffer(blob);
                }

                var piece;
                reader.onload = function reader_onload(e) {
                    fileLoaded += e.total;
                    if (fileLoaded < fileSize) {
                        if (e.target.result) {
                            piece = new Uint8Array(e.target.result);
                            _check_aborted(session);
                            xfer.send(piece);
                            if (options.on_progress) {
                                options.on_progress(cur_b.obj, xfer, piece);
                            }
                        }
                        readBlob();
                    } else {
                        //
                        if (e.target.result) {
                            piece = new Uint8Array(e.target.result);
                            _check_aborted(session);
                            xfer.end(piece).then(function () {
                                if (options.on_progress && piece.length) {
                                    options.on_progress(cur_b.obj, xfer, piece);
                                }
                                if (options.on_file_complete) {
                                    options.on_file_complete(cur_b.obj, xfer);
                                }
                                res(promise_callback());
                            })
                        }
                    }
                };
                readBlob();
            });
        });
    }

    return promise_callback();
}
