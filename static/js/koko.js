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

function initTerminal(elementId) {
    let urlParams = new URLSearchParams(window.location.search);
    let scheme = document.location.protocol == "https:" ? "wss" : "ws";
    let port = document.location.port ? ":" + document.location.port : "";
    let baseWsUrl = scheme + "://" + document.location.hostname + port + "/koko/ws/?"
    let pingInterval;
    let resizeTimer;
    let term;
    let lastSendTime;
    let lastReceiveTime;
    let initialed;
    let ws;
    let terminalId = "";
    let wsURL = baseWsUrl + urlParams.toString();
    ws = new WebSocket(wsURL, ["JMS-KOKO"]);
    term = createTerminalById(elementId)

    function resizeTerminal() {
        // 延迟调整窗口大小
        if (resizeTimer != null) {
            clearTimeout(resizeTimer);
        }
        resizeTimer = setTimeout(function () {
            document.getElementById('terminal').style.height = window.innerHeight + 'px';
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
                break
            case "CLOSE":
                term.writeln("Connection closed");
                let e = new Event("CLOSE", {})
                window.dispatchEvent(e)
                break
            case "PING":
                break
            case 'TERMINAL_DATA':
                term.write(msg.data);
                break
            default:
                console.log(data)
        }
    }

    window.SendTerminalData = function(data){
        if (ws.readyState === WebSocket.CLOSING ||
            ws.readyState === WebSocket.CLOSED) {
            return
        }
        ws.send(message(terminalId, 'TERMINAL_DATA', data));
    }

    window.addEventListener('resize', resizeTerminal);

    term.on('data', data => {
        if (initialed === null || ws === null) {
            return
        }
        lastSendTime = new Date();
        ws.send(message(terminalId, 'TERMINAL_DATA', data));
    });

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
        handleError(e)
    }
    ws.onclose = (e) => {
        term.writeln("Connection closed");
        handleError(e)
    }
    ws.onmessage = (e) => {
        lastReceiveTime = new Date();
        dispatch(term, e.data);
    }
}

function createTerminalById(elementId) {
    let fontSize = 14
    if (!fontSize || fontSize < 5 || fontSize > 50) {
        fontSize = 13;
    }
    document.getElementById(elementId).style.height = window.innerHeight + 'px';
    fit.apply(Terminal)
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
    return term
}
