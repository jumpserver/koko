const scheme = document.location.protocol === "https:" ? "wss" : "ws";
const port = document.location.port ? ':' + document.location.port : '';
const BASE_WS_URL = scheme + '://' + document.location.hostname + port;
const BASE_URL = document.location.protocol + '//' + document.location.hostname + port;
export {BASE_WS_URL, BASE_URL}

export function decodeToStr(octets) {
    if (typeof TextEncoder == "function") {
        return new TextDecoder("utf-8").decode(new Uint8Array(octets))
    }
    return decodeURIComponent(escape(String.fromCharCode.apply(null, octets)));
}

export function fireEvent(e) {
    window.dispatchEvent(e)
}

export function bytesHuman(bytes, precision) {
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

export function CopyTextToClipboard(text) {
    let transfer = document.createElement('textarea');
    document.body.appendChild(transfer);
    transfer.value = text;
    transfer.focus();
    transfer.select();
    document.execCommand('copy');
    document.body.removeChild(transfer);
}

// 使用 ES6 的函数默认值方式设置参数的默认取值
// 具体参见 https://developer.mozilla.org/zh-CN/docs/Web/JavaScript/Reference/Functions/Default_parameters

export function canvasWaterMark({
                                    container = document.body,
                                    width = 300,
                                    height = 300,
                                    textAlign = 'center',
                                    textBaseline = 'middle',
                                    alpha = 0.3,
                                    font = '20px monaco, microsoft yahei',
                                    fillStyle = 'rgba(184, 184, 184, 0.8)',
                                    content = 'JumpServer',
                                    rotate = -45,
                                    zIndex = 1000
                                }) {
    const canvas = document.createElement('canvas');
    const ctx = canvas.getContext('2d');

    canvas.width = width;
    canvas.height = height;
    ctx.globalAlpha = 0.5;

    ctx.font = font;
    ctx.fillStyle = fillStyle;
    ctx.textAlign = textAlign;
    ctx.textBaseline = textBaseline;
    ctx.globalAlpha = alpha;

    ctx.translate(0.5 * width, 0.5 * height);
    ctx.rotate((rotate * Math.PI) / 180);

    function generateMultiLineText(_ctx, _text, _width, _lineHeight) {
        const words = _text.split('\n');
        let line = '';
        const x = 0;
        let y = 0;

        for (let n = 0; n < words.length; n++) {
            line = words[n];
            line = truncateCenter(line, 25);
            _ctx.fillText(line, x, y);
            y += _lineHeight;
        }
    }

    generateMultiLineText(ctx, content, width, 24);

    const base64Url = canvas.toDataURL();
    const watermarkDiv = document.createElement('div');
    watermarkDiv.setAttribute('style', `
            position:absolute;
            top:0;
            left:0;
            width:100%;
            height:100%;
            z-index:${zIndex};
            pointer-events:none;
            background-repeat:repeat;
            background-image:url('${base64Url}')`
    );

    container.style.position = 'relative';
    container.insertBefore(watermarkDiv, container.firstChild);
}

function truncateCenter(s, l) {
    if (s.length <= l) {
        return s;
    }
    const centerIndex = Math.ceil(l / 2);
    return s.slice(0, centerIndex - 2) + '...' + s.slice(centerIndex + 1, l);
}