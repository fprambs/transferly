import type { RemoteEntry } from '@/lib/api'

/**
 * What kind of file a name suggests, which picks the sentence that describes it and its icon (4.14e). The sentence of most kinds takes the
 * name of the format, such as "PNG image" or "Python script".
 */
export type FileKind =
  | 'image'
  | 'audio'
  | 'video'
  | 'archive'
  | 'package'
  | 'disk'
  | 'font'
  | 'code'
  | 'script'
  | 'document'
  | 'spreadsheet'
  | 'presentation'
  | 'data'
  | 'config'
  | 'text'
  | 'log'
  | 'backup'
  | 'key'
  | 'certificate'
  | 'executable'
  | 'named'
  | 'unknown'
  | 'plain'

/** The type of an entry: its kind and the name of its format, which the sentence of the kind may use. */
export interface FileType {
  /** Kind of the file, or of the entry when it is not a file. */
  kind: FileKind | 'directory' | 'symlink' | 'other'
  /** Name of the format, such as PNG, Python or TAR.GZ. */
  name: string
}

/** Formats of each kind: extensions, lower case, with the format name when it is not the extension in upper case. */
const formats: ReadonlyArray<readonly [FileKind, ReadonlyArray<string | readonly [string, string]>]> = [
  ['image', ['png', ['jpg', 'JPEG'], 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'ico', ['tif', 'TIFF'], 'tiff', 'avif', 'heic', 'psd', 'raw', 'eps']],
  ['audio', ['mp3', 'wav', 'flac', 'ogg', 'oga', 'm4a', 'aac', 'opus', 'wma', 'mid', 'midi']],
  ['video', ['mp4', 'mkv', 'webm', 'mov', 'avi', 'wmv', 'flv', 'm4v', 'mpeg', 'mpg', '3gp', 'ogv']],
  ['archive', ['zip', ['gz', 'GZIP'], ['tgz', 'TAR.GZ'], 'tar', 'bz2', 'xz', 'zst', '7z', 'rar', 'lz', 'lzma', 'cab', 'jar', 'war']],
  ['package', ['deb', 'rpm', 'apk', 'dmg', 'msi', 'pkg', 'snap', 'appimage', 'whl', 'gem', 'nupkg']],
  ['disk', ['iso', 'img', 'vmdk', 'vdi', 'qcow2', 'vhd', 'vhdx']],
  ['font', ['ttf', 'otf', 'woff', 'woff2', 'eot']],
  [
    'code',
    [
      ['js', 'JavaScript'],
      ['mjs', 'JavaScript'],
      ['cjs', 'JavaScript'],
      ['jsx', 'JSX'],
      ['ts', 'TypeScript'],
      ['tsx', 'TSX'],
      ['html', 'HTML'],
      ['htm', 'HTML'],
      ['css', 'CSS'],
      ['scss', 'Sass'],
      ['sass', 'Sass'],
      ['less', 'Less'],
      ['vue', 'Vue'],
      ['svelte', 'Svelte'],
      ['go', 'Go'],
      ['rs', 'Rust'],
      ['java', 'Java'],
      ['kt', 'Kotlin'],
      ['swift', 'Swift'],
      ['c', 'C'],
      ['h', 'C'],
      ['cpp', 'C++'],
      ['cc', 'C++'],
      ['hpp', 'C++'],
      ['cs', 'C#'],
      ['scala', 'Scala'],
      ['dart', 'Dart'],
      ['ex', 'Elixir'],
      ['exs', 'Elixir'],
      ['erl', 'Erlang'],
      ['hs', 'Haskell'],
      ['clj', 'Clojure'],
      ['twig', 'Twig'],
      ['tpl', 'Template'],
      ['blade', 'Blade'],
    ],
  ],
  [
    'script',
    [
      ['sh', 'Shell'],
      ['bash', 'Bash'],
      ['zsh', 'Zsh'],
      ['fish', 'Fish'],
      ['py', 'Python'],
      ['rb', 'Ruby'],
      ['php', 'PHP'],
      ['pl', 'Perl'],
      ['lua', 'Lua'],
      ['r', 'R'],
      ['ps1', 'PowerShell'],
      ['bat', 'Batch'],
      ['cmd', 'Batch'],
      ['awk', 'AWK'],
    ],
  ],
  ['document', ['pdf', ['doc', 'Word'], ['docx', 'Word'], ['odt', 'OpenDocument'], ['rtf', 'RTF'], ['md', 'Markdown'], ['markdown', 'Markdown'], ['rst', 'reStructuredText'], ['tex', 'LaTeX'], ['epub', 'EPUB']]],
  ['spreadsheet', [['xls', 'Excel'], ['xlsx', 'Excel'], ['ods', 'OpenDocument'], 'csv', 'tsv']],
  ['presentation', [['ppt', 'PowerPoint'], ['pptx', 'PowerPoint'], ['odp', 'OpenDocument']]],
  ['data', ['json', 'xml', ['yml', 'YAML'], 'yaml', 'toml', 'sql', ['sqlite', 'SQLite'], ['db', 'Database'], 'graphql', 'proto', 'ndjson', 'parquet', 'avro']],
  ['config', ['ini', 'conf', 'cfg', ['env', 'dotenv'], 'properties', ['service', 'systemd'], ['htaccess', 'Apache'], 'plist', 'lock']],
  ['text', ['txt', 'text', 'nfo']],
  ['log', ['log', 'out']],
  ['backup', ['bak', 'old', 'orig', 'swp', 'tmp']],
  ['key', [['key', 'PEM'], ['pub', 'SSH'], ['ppk', 'PuTTY'], ['asc', 'PGP'], ['gpg', 'PGP'], ['kdbx', 'KeePass']]],
  ['certificate', ['pem', 'crt', 'cer', 'csr', 'der', ['p12', 'PKCS#12'], ['pfx', 'PKCS#12']]],
  ['executable', ['exe', 'dll', 'so', 'bin', 'app', 'elf', 'o', 'class', 'wasm']],
]

/** Every known extension with its kind and format name. */
const byExtension: ReadonlyMap<string, FileType> = new Map(
  formats.flatMap(([kind, list]) => list.map((item) => (typeof item === 'string' ? ([item, { kind, name: item.toUpperCase() }] as const) : ([item[0], { kind, name: item[1] }] as const)))),
)

/** Extensions that follow a .tar, which make a compressed tarball. */
const tarballs: ReadonlySet<string> = new Set(['gz', 'bz2', 'xz', 'zst', 'lz', 'lzma', 'z'])

/** Files known by their whole name, lower case, which have no extension or whose dot does not start one. */
const byName: ReadonlyMap<string, FileType> = new Map<string, FileType>([
  ['dockerfile', { kind: 'named', name: 'Dockerfile' }],
  ['makefile', { kind: 'named', name: 'Makefile' }],
  ['vagrantfile', { kind: 'named', name: 'Vagrantfile' }],
  ['jenkinsfile', { kind: 'named', name: 'Jenkinsfile' }],
  ['procfile', { kind: 'named', name: 'Procfile' }],
  ['gemfile', { kind: 'named', name: 'Gemfile' }],
  ['readme', { kind: 'text', name: 'README' }],
  ['license', { kind: 'text', name: 'LICENSE' }],
  ['changelog', { kind: 'text', name: 'CHANGELOG' }],
  ['.htaccess', { kind: 'config', name: 'Apache' }],
  ['.htpasswd', { kind: 'config', name: 'Apache' }],
  ['.gitignore', { kind: 'config', name: 'Git' }],
  ['.gitattributes', { kind: 'config', name: 'Git' }],
  ['.gitmodules', { kind: 'config', name: 'Git' }],
  ['.dockerignore', { kind: 'config', name: 'Docker' }],
  ['.editorconfig', { kind: 'config', name: 'EditorConfig' }],
  ['.npmrc', { kind: 'config', name: 'npm' }],
  ['.env', { kind: 'config', name: 'dotenv' }],
  ['.bashrc', { kind: 'script', name: 'Bash' }],
  ['.bash_profile', { kind: 'script', name: 'Bash' }],
  ['.profile', { kind: 'script', name: 'Shell' }],
  ['.zshrc', { kind: 'script', name: 'Zsh' }],
  ['authorized_keys', { kind: 'key', name: 'SSH' }],
  ['known_hosts', { kind: 'key', name: 'SSH' }],
  ['id_rsa', { kind: 'key', name: 'SSH' }],
  ['id_ed25519', { kind: 'key', name: 'SSH' }],
  ['id_ecdsa', { kind: 'key', name: 'SSH' }],
])

/**
 * The type of an entry from its name, as FileZilla tells it (4.14e): nothing is read from the file. Folders and links are their own
 * types. A file named after a known one (Dockerfile, .htaccess) takes its type; otherwise its extension decides, a compressed tarball
 * (.tar.gz) counting as one; an extension this table does not know is "XYZ file", and a file without one is a plain file.
 */
export function fileTypeOf(entry: Pick<RemoteEntry, 'name' | 'type'>): FileType {
  if (entry.type !== 'file') {
    return { kind: entry.type, name: '' }
  }
  const lower = entry.name.toLowerCase()
  const named = byName.get(lower)
  if (named !== undefined) {
    return named
  }
  const parts = lower.split('.')
  const extension = parts.length > (parts[0] === '' ? 2 : 1) ? (parts.at(-1) as string) : ''
  if (extension === '') {
    return { kind: 'plain', name: '' }
  }
  if (tarballs.has(extension) && parts[parts.length - 2] === 'tar') {
    return { kind: 'archive', name: `TAR.${extension.toUpperCase()}` }
  }
  return byExtension.get(extension) ?? { kind: 'unknown', name: extension.toUpperCase() }
}
