/** Isolated bridge for the shell-owned About document. */
import { contextBridge, ipcRenderer } from 'electron'
import { ABOUT_IPC, type DesktopAboutApi, type DesktopAboutView } from './about-contract.ts'

const api: DesktopAboutApi = {
  status: () => ipcRenderer.invoke(ABOUT_IPC.status) as Promise<DesktopAboutView>,
  copyPackageId: () => ipcRenderer.invoke(ABOUT_IPC.copyPackageId) as Promise<boolean>,
  open: url => ipcRenderer.invoke(ABOUT_IPC.open, url) as Promise<void>,
  subscribe: (listener) => {
    const receive = (_event: Electron.IpcRendererEvent, view: DesktopAboutView): void => { listener(view) }
    ipcRenderer.on(ABOUT_IPC.changed, receive)
    return () => { ipcRenderer.removeListener(ABOUT_IPC.changed, receive) }
  },
}
if (location.href === 'dsh-app://shell/about.html') contextBridge.exposeInMainWorld('dshAbout', api)
