/** The asm.js build of sql.js, which runs without WebAssembly and has the same API as the default one. */
declare module 'sql.js/dist/sql-asm.js' {
  import initSqlJs from 'sql.js'

  export default initSqlJs
}
