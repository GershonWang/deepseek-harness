/** 托盘「终端」窗口的隔离桥：只暴露终端的调用与两个事件流。 */
import { contextBridge, ipcRenderer } from 'electron'
import { TERMINAL_IPC, type DesktopTerminalApi, type DesktopTerminalView } from './terminal-contract.ts'

const api: DesktopTerminalApi = {
  status: () => ipcRenderer.invoke(TERMINAL_IPC.status) as Promise<DesktopTerminalView>,
  start: (cols, rows) => ipcRenderer.invoke(TERMINAL_IPC.start, cols, rows) as Promise<DesktopTerminalView>,
  write: data => ipcRenderer.invoke(TERMINAL_IPC.write, data) as Promise<void>,
  resize: (cols, rows) => ipcRenderer.invoke(TERMINAL_IPC.resize, cols, rows) as Promise<void>,
  readClipboard: () => ipcRenderer.invoke(TERMINAL_IPC.clipboardRead) as Promise<string>,
  writeClipboard: text => ipcRenderer.invoke(TERMINAL_IPC.clipboardWrite, text) as Promise<void>,
  subscribe: (listener) => {
    const receive = (_event: Electron.IpcRendererEvent, view: DesktopTerminalView): void => { listener(view) }
    ipcRenderer.on(TERMINAL_IPC.state, receive)
    return () => { ipcRenderer.removeListener(TERMINAL_IPC.state, receive) }
  },
  output: (listener) => {
    const receive = (_event: Electron.IpcRendererEvent, data: string): void => { listener(data) }
    ipcRenderer.on(TERMINAL_IPC.output, receive)
    return () => { ipcRenderer.removeListener(TERMINAL_IPC.output, receive) }
  },
}
// 这个 preload 只挂在终端窗口上，而该窗口禁止一切导航；因此不需要按 URL 再判一次。
contextBridge.exposeInMainWorld('dshTerminal', api)
