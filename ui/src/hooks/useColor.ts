import { ref } from 'vue';

interface HSL {
  h: number;
  s: number;
  l: number;
}

const mainThemeColorMap = new Map(
  Object.entries({
    default: '#483D3D',
    deepBlue: '#1A212C',
    darkGary: '#303237',
  })
);

export const useColor = () => {
  const currentMainColoc = ref('');

  const setCurrentMainColor = (color: string) => {
    const themeColor = mainThemeColorMap.get(color);

    if (themeColor) {
      currentMainColoc.value = themeColor;
    } else {
      currentMainColoc.value = '#483D3D';
    }
  };

  /**
   * 将十六进制颜色转换为HSL颜色
   * @param hex 十六进制颜色
   * @returns HSL颜色
   */
  const hexToHSL = (hex: string): HSL => {
    let hexValue = hex.replace(/^#/, '');

    if (hexValue.length === 3) {
      hexValue = hexValue
        .split('')
        .map(char => char + char)
        .join('');
    }

    // 解析RGB值
    const r = Number.parseInt(hexValue.substring(0, 2), 16) / 255;
    const g = Number.parseInt(hexValue.substring(2, 4), 16) / 255;
    const b = Number.parseInt(hexValue.substring(4, 6), 16) / 255;

    // 计算HSL值
    const max = Math.max(r, g, b);
    const min = Math.min(r, g, b);
    let h = 0;
    let s = 0;
    const l = (max + min) / 2;

    if (max !== min) {
      const d = max - min;
      s = l > 0.5 ? d / (2 - max - min) : d / (max + min);

      switch (max) {
        case r:
          h = (g - b) / d + (g < b ? 6 : 0);
          break;
        case g:
          h = (b - r) / d + 2;
          break;
        case b:
          h = (r - g) / d + 4;
          break;
      }

      h /= 6;
    }

    // 转换为标准HSL格式
    return {
      h: Math.round(h * 360),
      s: Math.round(s * 100),
      l: Math.round(l * 100),
    };
  };

  /**
   * 将HSL颜色转换为十六进制颜色
   * @param h 色相
   * @param s 饱和度
   * @param l 亮度
   * @returns 十六进制颜色
   */
  const hslToHex = (h: number, s: number, l: number) => {
    h /= 360;
    s /= 100;
    l /= 100;

    let r, g, b;

    if (s === 0) {
      // 如果饱和度为0，则为灰色
      r = g = b = l;
    } else {
      const hue2rgb = (p: number, q: number, t: number): number => {
        if (t < 0) t += 1;
        if (t > 1) t -= 1;
        if (t < 1 / 6) return p + (q - p) * 6 * t;
        if (t < 1 / 2) return q;
        if (t < 2 / 3) return p + (q - p) * (2 / 3 - t) * 6;
        return p;
      };

      const q = l < 0.5 ? l * (1 + s) : l + s - l * s;
      const p = 2 * l - q;

      r = hue2rgb(p, q, h + 1 / 3);
      g = hue2rgb(p, q, h);
      b = hue2rgb(p, q, h - 1 / 3);
    }

    // 转换为十六进制
    const toHex = (x: number): string => {
      const hex = Math.round(x * 255).toString(16);
      return hex.length === 1 ? `0${hex}` : hex;
    };

    return `#${toHex(r)}${toHex(g)}${toHex(b)}`;
  };

  /**
   * 将颜色转换为rgba格式
   * @param alphaValue 透明度值
   * @param color 颜色
   * @returns rgba格式颜色
   */
  const alpha = (alphaValue: number, color?: string) => {
    // 如果没有提供颜色，使用当前主题颜色
    const actualColor = color || currentMainColoc.value;
    // 确保透明度值在0-1之间
    const alpha = Math.max(0, Math.min(1, alphaValue));

    // 移除#号并处理缩写形式
    let hex = actualColor.replace(/^#/, '');

    if (hex.length === 3) {
      hex = hex
        .split('')
        .map(char => char + char)
        .join('');
    }

    // 解析RGB值
    const r = Number.parseInt(hex.substring(0, 2), 16);
    const g = Number.parseInt(hex.substring(2, 4), 16);
    const b = Number.parseInt(hex.substring(4, 6), 16);

    // 返回rgba格式
    return `rgba(${r}, ${g}, ${b}, ${alpha})`;
  };

  /**
   * 将颜色变亮
   * @param amount
   * @param color
   * @param alphaValue
   * @returns
   */
  const lighten = (amount: number, color?: string, alphaValue?: number) => {
    const actualColor = color || currentMainColoc.value;
    const hsl = hexToHSL(actualColor);
    const hexColor = hslToHex(hsl.h, hsl.s, Math.min(100, hsl.l + amount));

    if (alphaValue !== undefined) {
      return alpha(alphaValue, hexColor);
    }

    return hexColor;
  };

  /**
   * 将颜色变暗
   * @param amount
   * @param color
   * @param alphaValue
   * @returns
   */
  const darken = (amount: number, color?: string, alphaValue?: number) => {
    const actualColor = color || currentMainColoc.value;
    const hsl = hexToHSL(actualColor);
    const hexColor = hslToHex(hsl.h, hsl.s, Math.max(0, hsl.l - amount));

    // 如果提供了透明度参数，应用透明度
    if (alphaValue !== undefined) {
      return alpha(alphaValue, hexColor);
    }

    return hexColor;
  };

  return {
    darken,
    lighten,
    alpha,
    setCurrentMainColor,
  };
};
