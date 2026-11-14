/** Isolated bridge for the shell-owned profile migration document. */
import { contextBridge, ipcRenderer } from 'electron'
import { MIGRATION_IPC, type MigrationApi, type MigrationView } from './migration-contract.ts'

const api: MigrationApi = {
  ready: () => ipcRenderer.invoke(MIGRATION_IPC.ready) as Promise<MigrationView>,
  respond: response => ipcRenderer.invoke(MIGRATION_IPC.respond, response) as Promise<void>,
  subscribe: (listener) => {
    const receive = (_event: Electron.IpcRendererEvent, view: MigrationView): void => { listener(view) }
    ipcRenderer.on(MIGRATION_IPC.changed, receive)
    return () => { ipcRenderer.removeListener(MIGRATION_IPC.changed, receive) }
  },
}
if (location.href === 'dsh-app://shell/migration.html') contextBridge.exposeInMainWorld('dshMigration', api)
