import { DirectiveBinding } from 'vue';

export const draggable = {
  beforeMount(el: HTMLElement, binding: DirectiveBinding) {
    let startX = 0;
    let startWidth = 0;

    const mouseMoveHandler = (event: MouseEvent) => {
      const newWidth = startWidth + (event.clientX - startX);

      // 确保宽度在合理范围内
      if (newWidth >= 300 && newWidth <= 600) {
        el.style.width = `${newWidth}px`;

        binding.value.width = newWidth;
      }
    };

    const mouseUpHandler = () => {
      document.removeEventListener('mousemove', mouseMoveHandler);
      document.removeEventListener('mouseup', mouseUpHandler);

      if (binding.value.onDragEnd && typeof binding.value.onDragEnd === 'function') {
        binding.value.onDragEnd(el, binding.value.width);
      }
    };

    const mouseDownHandler = (event: MouseEvent) => {
      const rect = el.getBoundingClientRect();
      // 只有在右侧边缘10px范围内拖动才触发
      if (event.clientX >= rect.right - 10 && event.clientX <= rect.right) {
        startX = event.clientX;
        startWidth = el.offsetWidth;

        document.addEventListener('mousemove', mouseMoveHandler);
        document.addEventListener('mouseup', mouseUpHandler);
      }
    };

    el.addEventListener('mousedown', mouseDownHandler);
  }
};
