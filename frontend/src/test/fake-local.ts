import { File as NodeFile } from 'node:buffer'

/** A file of a fake folder. */
export class FakeFile {
  /** The kind of handle, as the browser names it. */
  readonly kind = 'file'
  /** Name of the file. */
  readonly name: string
  /** What the file holds. */
  content: Uint8Array
  /** Modification time in milliseconds. */
  modified = Date.UTC(2026, 2, 8, 7, 30)
  /** Whether writes to the file fail, as with a full disk. */
  readonly failWrites: boolean

  /** Creates a file with a name and some content. */
  constructor(name: string, content = '', failWrites = false) {
    this.name = name
    this.failWrites = failWrites
    this.content = new TextEncoder().encode(content)
  }

  /** Reads the file as the browser does. */
  getFile(): Promise<File> {
    return Promise.resolve(new NodeFile([this.content], this.name, { lastModified: this.modified }) as unknown as File)
  }

  /** Opens a stream that replaces the content, or fails when the file was made to fail. */
  createWritable(): Promise<WritableStream<Uint8Array>> {
    const chunks: Uint8Array[] = []
    return Promise.resolve(
      new WritableStream<Uint8Array>({
        write: (chunk) => {
          if (this.failWrites) {
            throw new Error('disk full')
          }
          chunks.push(chunk)
        },
        close: () => {
          this.content = new Uint8Array(chunks.flatMap((chunk) => [...chunk]))
        },
      }),
    )
  }
}

/** A folder of the user's computer that lives in memory. */
export class FakeDir {
  /** The kind of handle, as the browser names it. */
  readonly kind = 'directory'
  /** What the folder holds, by name. */
  readonly children = new Map<string, FakeDir | FakeFile>()
  /** Name of the folder. */
  readonly name: string
  /** Whether files created in it refuse to be written. */
  failWrites = false

  /** Creates a folder with a name. */
  constructor(name: string) {
    this.name = name
  }

  /** Adds a file and returns the folder, so calls chain. */
  withFile(name: string, content = ''): this {
    this.children.set(name, new FakeFile(name, content))
    return this
  }

  /** Adds a folder and returns it. */
  withDir(name: string): FakeDir {
    const dir = new FakeDir(name)
    this.children.set(name, dir)
    return dir
  }

  /** Opens a folder of this one, creating it on request. */
  getDirectoryHandle(name: string, options?: { create?: boolean }): Promise<FakeDir> {
    const found = this.children.get(name)
    if (found instanceof FakeDir) {
      return Promise.resolve(found)
    }
    if (found === undefined && options?.create) {
      return Promise.resolve(this.withDir(name))
    }
    return Promise.reject(new DOMException(name, 'NotFoundError'))
  }

  /** Opens a file of this folder, creating it on request. */
  getFileHandle(name: string, options?: { create?: boolean }): Promise<FakeFile> {
    const found = this.children.get(name)
    if (found instanceof FakeFile) {
      return Promise.resolve(found)
    }
    if (found === undefined && options?.create) {
      const file = new FakeFile(name, '', this.failWrites)
      this.children.set(name, file)
      return Promise.resolve(file)
    }
    return Promise.reject(new DOMException(name, 'NotFoundError'))
  }

  /** Removes an entry of this folder. */
  removeEntry(name: string): Promise<void> {
    this.children.delete(name)
    return Promise.resolve()
  }

  /** Lists the entries the way the browser does. */
  async *entries(): AsyncGenerator<[string, FakeDir | FakeFile]> {
    for (const entry of this.children) {
      yield entry
    }
  }
}

/** Makes the browser offer a folder picker that answers with a folder, or with a closed dialog when there is none. */
export function offerPicker(answer: FakeDir | null): void {
  Object.defineProperty(window, 'showDirectoryPicker', {
    configurable: true,
    value: () => (answer === null ? Promise.reject(new DOMException('closed', 'AbortError')) : Promise.resolve(answer)),
  })
}

/** Makes the browser offer no folder picker, as Firefox and Safari do. */
export function withdrawPicker(): void {
  Reflect.deleteProperty(window, 'showDirectoryPicker')
}

/** Presents a fake folder as the handle the browser gives. */
export function handleOf(dir: FakeDir): FileSystemDirectoryHandle {
  return dir as unknown as FileSystemDirectoryHandle
}
