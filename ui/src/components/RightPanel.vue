<template>
  <div
    ref="container"
    :class="{show: show}"
    class="container"
  >
    <div class="background" />
    <div class="right-panel">
      <div ref="dragDiv" class="handle-button">
        <i :class="show ? 'el-icon-close':'el-icon-setting'" />
      </div>
      <div class="right-panel-items">
        <slot />
      </div>
    </div>
  </div>
</template>

<script>
import { addClass, removeClass } from '@/utils/common'

export default {
  name: 'RightPanel',
  props: {
    clickNotClose: {
      type: Boolean,
      default: false
    }
  },
  data() {
    return {
      show: false
    }
  },
  watch: {
    show(value) {
      if (value && !this.clickNotClose) {
        this.addEventClick()
      }
      if (value) {
        addClass(document.body, 'showRightPanel')
      } else {
        removeClass(document.body, 'showRightPanel')
      }
    }
  },
  mounted() {
    this.init()
    this.insertToBody()
  },
  beforeDestroy() {
    const element = this.$refs.container
    element.remove()
  },
  methods: {
    init() {
      this.$nextTick(() => {
        const dragDiv = this.$refs.dragDiv;
        const clientOffset = {};
        dragDiv.addEventListener("mousedown", (event) => {
          const offsetX = dragDiv.getBoundingClientRect().left;
          const offsetY = dragDiv.getBoundingClientRect().top;
          const innerX = event.clientX - offsetX;
          const innerY = event.clientY - offsetY;

          clientOffset.clientX = event.clientX;
          clientOffset.clientY = event.clientY;
          document.onmousemove = function(event) {
            dragDiv.style.left = event.clientX - innerX + "px";
            dragDiv.style.top = event.clientY - innerY + "px";
            const dragDivTop = window.innerHeight - dragDiv.getBoundingClientRect().height;
            const dragDivLeft = window.innerWidth - dragDiv.getBoundingClientRect().width;
            dragDiv.style.left = dragDivLeft + "px";
            dragDiv.style.left =  "-48px";
            if (dragDiv.getBoundingClientRect().top <= 0) {
              dragDiv.style.top = "0px";
            }
            if (dragDiv.getBoundingClientRect().top >= dragDivTop) {
              dragDiv.style.top = dragDivTop + "px";
            }
          };
          document.onmouseup = function() {
            document.onmousemove = null;
            document.onmouseup = null;
          };
        }, false);
        dragDiv.addEventListener('mouseup', (event) => {
          const clientX = event.clientX;
          const clientY = event.clientY;
          if (clientX === clientOffset.clientX && clientY === clientOffset.clientY) {
            this.show = !this.show
          }
        })
      })
    },
    addEventClick() {
      window.addEventListener('click', this.closeSidebar)
    },
    closeSidebar(evt) {
      const parent = evt.target.closest('.right-panel')
      if (!parent) {
        this.show = false
        window.removeEventListener('click', this.closeSidebar)
      }
    },
    insertToBody() {
      const element = this.$refs.container
      const body = document.querySelector('body')
      body.insertBefore(element, body.firstChild)
    }
  }
}
</script>

<style scoped>
.background {
  position: fixed;
  top: 0;
  left: 0;
  opacity: 0;
  transition: opacity .3s cubic-bezier(.7, .3, .1, 1);
  background: rgba(0, 0, 0, .3);
  z-index: -1;
}

.right-panel {
  width: 100%;
  max-width: 260px;
  height: 100vh;
  position: fixed;
  top: 0;
  right: 0;
  box-shadow: 0 0 15px 0 rgba(0, 0, 0, .05);
  transition: all .25s cubic-bezier(.7, .3, .1, 1);
  transform: translate(100%);
  background: #fff;
  z-index: 1200;
}

.show {
  transition: all .3s cubic-bezier(.7, .3, .1, 1);
}

.show .background {
  z-index: 1000;
  opacity: 1;
  width: 100%;
  height: 100%;
}

.show .right-panel {
  transform: translate(0);
}

.handle-button {
  position: absolute;
  top: 30%;
  left: -48px;
  width: 48px;
  height: 45px;
  line-height: 45px;
  box-sizing: border-box;
  text-align: center;
  font-size: 24px;
  border-radius: 20px 0 0 20px;
  z-index: 0;
  pointer-events: auto;
  color: #fff;
  opacity: .8;
  background-color: rgba(245, 247, 250, 0.3)
}

.handle-button:hover {
  cursor: pointer;
  background-color: rgba(245, 247, 250, 0.4)
}

.handle-button i {
  font-size: 20px;
  line-height: 45px;
}
</style>
