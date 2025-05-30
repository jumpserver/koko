import { useEffect, useRef } from 'react';
import * as monaco from 'monaco-editor';

const Editor = () => {
  const editorRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (editorRef.current) {
      const editor = monaco.editor.create(editorRef.current, {
        value: 'console.log("Hello, world")',
        language: 'javascript',
        theme: 'vs-dark', // 可选：设置暗色主题
        automaticLayout: true, // 自动调整布局
        minimap: { enabled: false }, // 可选：禁用小地图
        scrollBeyondLastLine: false // 可选：禁用最后一行后的滚动
      });

      return () => {
        editor.dispose();
      };
    }
  }, []);

  return (
    <div
      ref={editorRef}
      style={{
        height: '100vh',
        width: '100%'
      }}
    />
  );
};

export default Editor;
