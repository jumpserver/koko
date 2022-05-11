<template>
  <div
    ref="rightPanel"
    :class="{show:show}"
    class="rightPanel-container"
  >
    <div class="rightPanel-background" />
    <div class="rightPanel">
      <div ref="dragDiv" class="handle-button">
        <i :class="show?'el-icon-close':'el-icon-setting'" />
      </div>
      <div class="rightPanel-items">
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
      default: false,
      type: Boolean
    }
  },
  data() {
    return {
      show: false
    }
  },
  computed: {
    theme() {
      return 'rgba(245, 247, 250, 0.2)'
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
    const elx = this.$refs.rightPanel
    elx.remove()
  },
  methods: {
    init() {
      this.$nextTick(() => {
      let dragDiv = this.$refs.dragDiv;
      let clientOffset = {};
      dragDiv.addEventListener("mousedown", (event) => {
        let offsetX = dragDiv.getBoundingClientRect().left;
        let offsetY = dragDiv.getBoundingClientRect().top;
        let innerX = event.clientX - offsetX;
        let innerY = event.clientY - offsetY;

        clientOffset.clientX = event.clientX;
        clientOffset.clientY = event.clientY;
        document.onmousemove = function(event) {
          dragDiv.style.left = event.clientX - innerX + "px";
          dragDiv.style.top = event.clientY - innerY + "px";
          let dragDivTop = window.innerHeight - dragDiv.getBoundingClientRect().height;
          let dragDivLeft = window.innerWidth - dragDiv.getBoundingClientRect().width;
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
        let clientX = event.clientX;
        let clientY = event.clientY;
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
      const parent = evt.target.closest('.rightPanel')
      if (!parent) {
        this.show = false
        window.removeEventListener('click', this.closeSidebar)
      }
    },
    insertToBody() {
      const elx = this.$refs.rightPanel
      const body = document.querySelector('body')
      body.insertBefore(elx, body.firstChild)
    }
  }
}
</script>

<style scoped>
.rightPanel-background {
  position: fixed;
  top: 0;
  left: 0;
  opacity: 0;
  transition: opacity .3s cubic-bezier(.7, .3, .1, 1);
  background: rgba(0, 0, 0, .3);
  z-index: -1;
}

.rightPanel {
  width: 100%;
  max-width: 260px;
  height: 100vh;
  position: fixed;
  top: 0;
  right: 0;
  box-shadow: 0px 0px 15px 0px rgba(0, 0, 0, .05);
  transition: all .25s cubic-bezier(.7, .3, .1, 1);
  transform: translate(100%);
  background: #fff;
  z-index: 1200;
}

.show {
  transition: all .3s cubic-bezier(.7, .3, .1, 1);
}

.show .rightPanel-background {
  z-index: 1000;
  opacity: 1;
  width: 100%;
  height: 100%;
}

.show .rightPanel {
  transform: translate(0);
}

.handle-button {
  position: absolute;
  top: 20%;
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
  cursor: move;
  color: #fff;
  opacity: .8;
  background-color: rgba(245, 247, 250, 0.2)
}

.handle-button:hover {
  cursor: pointer;
  background-color: rgba(245, 247, 250, 0.3)
}

.handle-button i {
  font-size: 20px;
  line-height: 45px;
}
</style>
