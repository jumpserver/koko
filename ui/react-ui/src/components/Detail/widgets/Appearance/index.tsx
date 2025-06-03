// @ts-ignore
import xtermTheme from 'xterm-theme';

import { useRef, useCallback } from 'react';
import { TextCursor, UserRoundPen } from 'lucide-react';
import { FontSizeOutlined, FontColorsOutlined } from '@ant-design/icons';
import { Card, Form, InputNumber, Segmented, Switch, Select, Space, Divider, Flex } from 'antd';

import useDetail from '@/store/useDetail';

const Appearance = () => {
  const { setTerminalConfig, terminalConfig } = useDetail();

  // prettier-ignore
  const updateConfig = useCallback((key: string, value: any) => {
      setTerminalConfig({ [key]: value });
    },
    [setTerminalConfig]
  );

  const themeOptions = useRef([
    { label: 'Default', value: 'Default' },
    ...Object.keys(xtermTheme).map(item => ({ label: item, value: item }))
  ]);

  const fontFamilyOptions = useRef([
    {
      label: <span>Sans Serif</span>,
      title: 'sans-serif',
      options: [{ label: <span>Open Sans</span>, value: 'Open Sans' }]
    },
    {
      label: <span>Monospace</span>,
      title: 'monospace',
      options: [
        { label: <span>Menlo</span>, value: 'Menlo' },
        { label: <span>JetBrains Mono</span>, value: 'JetBrains Mono' },
        { label: <span>Consolas</span>, value: 'Consolas' },
        { label: <span>Fira Code</span>, value: 'Fira Code' },
        { label: <span>Source Code Pro</span>, value: 'Source Code Pro' },
        { label: <span>Monaco</span>, value: 'Monaco' }
      ]
    }
  ]);

  const cursorStyleOptions = [
    { value: 'block', label: '块状' },
    { value: 'underline', label: '下划线' },
    { value: 'bar', label: '竖线' }
  ];

  return (
    <Card className="appearance-card" variant="borderless">
      <Space direction="vertical" size="large" style={{ width: '100%' }}>
        <Form layout="vertical" style={{ width: '100%' }}>
          {/* 字体设置 */}
          <div style={{ marginBottom: '16px' }}>
            <Divider orientation="left" plain style={{ margin: '8px 0' }}>
              <Space>
                <FontColorsOutlined />
                <span>字体</span>
              </Space>
            </Divider>

            <Flex gap={16} align="start">
              <Form.Item label="字体系列" style={{ flex: 3, marginBottom: '8px' }}>
                <Select
                  placeholder="选择字体"
                  value={terminalConfig.fontFamily}
                  options={fontFamilyOptions.current}
                  onChange={value => updateConfig('fontFamily', value)}
                />
              </Form.Item>

              <Form.Item label="字体大小" style={{ flex: 1, marginBottom: '8px' }}>
                <InputNumber
                  min={8}
                  max={32}
                  keyboard={true}
                  value={terminalConfig.fontSize}
                  onChange={value => updateConfig('fontSize', value)}
                  prefix={<FontSizeOutlined />}
                  style={{ width: '100%' }}
                />
              </Form.Item>
            </Flex>
          </div>

          {/* 光标设置 */}
          <div>
            <Divider orientation="left" plain style={{ margin: '8px 0' }}>
              <Space>
                <TextCursor size={14} />
                <span>光标</span>
              </Space>
            </Divider>

            <Flex gap={16} align="start">
              <Form.Item label="光标样式" style={{ flex: 3, marginBottom: '8px' }}>
                <Segmented
                  options={cursorStyleOptions}
                  value={terminalConfig.cursorStyle}
                  onChange={value => updateConfig('cursorStyle', value)}
                />
              </Form.Item>

              <Form.Item label="光标闪烁" style={{ flex: 1, marginBottom: '8px' }}>
                <Switch
                  size="default"
                  checked={terminalConfig.cursorBlink}
                  onChange={checked => updateConfig('cursorBlink', checked)}
                />
              </Form.Item>
            </Flex>
          </div>

          {/* 主题设置 */}
          <div>
            <Divider orientation="left" plain style={{ margin: '8px 0' }}>
              <Space>
                <UserRoundPen size={14} />
                <span>偏好</span>
              </Space>
            </Divider>

            <Form.Item label="主题" style={{ flex: 3, marginBottom: '8px' }}>
              <Select
                options={themeOptions.current}
                value={terminalConfig.theme}
                onChange={value => updateConfig('theme', value)}
              />
            </Form.Item>
          </div>
        </Form>
      </Space>
    </Card>
  );
};

export default Appearance;
