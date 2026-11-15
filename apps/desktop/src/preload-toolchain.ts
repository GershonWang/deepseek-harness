/** Isolated bridge for the shell-owned toolchain document. */
import { contextBridge, ipcRenderer } from 'electron'
import {
  TOOLCHAIN_IPC,
  type ToolchainApi,
  type ToolchainProgressView,
  type ToolchainView,
} from './toolchain-contract.ts'

const api: ToolchainApi = {
  status: () => ipcRenderer.invoke(TOOLCHAIN_IPC.status) as Promise<ToolchainView>,
  install: (id, version) => ipcRenderer.invoke(TOOLCHAIN_IPC.install, id, version) as Promise<void>,
  cancel: () => ipcRenderer.invoke(TOOLCHAIN_IPC.cancel) as Promise<void>,
  uninstall: (id, version) => ipcRenderer.invoke(TOOLCHAIN_IPC.uninstall, id, version) as Promise<void>,
  activate: (id, version) => ipcRenderer.invoke(TOOLCHAIN_IPC.activate, id, version) as Promise<void>,
  deactivate: id => ipcRenderer.invoke(TOOLCHAIN_IPC.deactivate, id) as Promise<void>,
  refresh: () => ipcRenderer.invoke(TOOLCHAIN_IPC.refresh) as Promise<void>,
  subscribe: (listener) => {
    const receive = (_event: Electron.IpcRendererEvent, view: ToolchainView): void => { listener(view) }
    ipcRenderer.on(TOOLCHAIN_IPC.changed, receive)
    return () => { ipcRenderer.removeListener(TOOLCHAIN_IPC.changed, receive) }
  },
  onProgress: (listener) => {
    const receive = (_event: Electron.IpcRendererEvent, progress: ToolchainProgressView): void => { listener(progress) }
    ipcRenderer.on(TOOLCHAIN_IPC.progress, receive)
    return () => { ipcRenderer.removeListener(TOOLCHAIN_IPC.progress, receive) }
  },
}
if (location.href === 'dsh-app://shell/toolchain.html') contextBridge.exposeInMainWorld('dshToolchain', api)
