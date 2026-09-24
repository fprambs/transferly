import ts from 'typescript'

const toolDirective = /^(\/\/\s*(@ts-|oxlint-|eslint-)|\/\/\/\s*<|\/\*\s*(oxlint-|eslint-))/

/** A documentation rule violation found in a source file. */
export interface Violation {
  /** Path of the offending file, as given to the rule. */
  file: string
  /** One-based line of the offending code or comment. */
  line: number
  /** Human-readable description of the violation. */
  message: string
}

/** A half-open source range delimited by two character offsets. */
interface Span {
  /** Offset of the first character of the range. */
  start: number
  /** Offset just after the last character of the range. */
  end: number
}

/** Parses TypeScript or TSX source text into a syntax tree with parent links. */
function parseSource(file: string, text: string): ts.SourceFile {
  const kind = file.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS
  return ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, kind)
}

/** Collects every comment in the source, visiting all nodes and tokens. */
function collectComments(source: ts.SourceFile): ts.CommentRange[] {
  const found = new Map<number, ts.CommentRange>()
  const add = (ranges: ts.CommentRange[] | undefined) => {
    for (const range of ranges ?? []) {
      found.set(range.pos, range)
    }
  }
  const visit = (node: ts.Node) => {
    add(ts.getLeadingCommentRanges(source.text, node.pos))
    add(ts.getTrailingCommentRanges(source.text, node.end))
    node.getChildren(source).forEach(visit)
  }
  visit(source)
  return [...found.values()]
}

/** Collects the source ranges of every function, method and arrow function body. */
function collectBodies(source: ts.SourceFile): Span[] {
  const spans: Span[] = []
  const visit = (node: ts.Node) => {
    if (ts.isFunctionLike(node)) {
      const body = (node as ts.FunctionLikeDeclaration).body
      if (body !== undefined) {
        spans.push({ start: body.pos, end: body.end })
      }
    }
    ts.forEachChild(node, visit)
  }
  visit(source)
  return spans
}

/** Builds a violation for a character offset of the source. */
function violationAt(source: ts.SourceFile, offset: number, message: string): Violation {
  const { line } = source.getLineAndCharacterOfPosition(offset)
  return { file: source.fileName, line: line + 1, message }
}

/** Reports whether a node carries a TSDoc comment. */
function hasDoc(node: ts.Node): boolean {
  return ts.getJSDocCommentsAndTags(node).length > 0
}

/** Reports whether an expression defines a function or a class. */
function isCallableInitializer(expression: ts.Expression | undefined): boolean {
  return (
    expression !== undefined &&
    (ts.isArrowFunction(expression) ||
      ts.isFunctionExpression(expression) ||
      ts.isClassExpression(expression))
  )
}

/** Reports whether a statement is a top-level `describe(...)` or `test.describe(...)` call. */
function isSuite(statement: ts.Statement): boolean {
  if (!ts.isExpressionStatement(statement) || !ts.isCallExpression(statement.expression)) {
    return false
  }
  const callee = statement.expression.expression
  if (ts.isIdentifier(callee)) {
    return callee.text === 'describe'
  }
  return (
    ts.isPropertyAccessExpression(callee) &&
    ts.isIdentifier(callee.expression) &&
    callee.expression.text === 'test' &&
    callee.name.text === 'describe'
  )
}

/** Reports whether a statement is exported. */
function isExported(statement: ts.Statement): boolean {
  return (
    ts.canHaveModifiers(statement) &&
    (ts.getModifiers(statement) ?? []).some((modifier) => modifier.kind === ts.SyntaxKind.ExportKeyword)
  )
}

/** Returns the members of a class or interface that must be documented. */
function documentedMembers(statement: ts.Statement): readonly ts.Node[] {
  if (ts.isClassDeclaration(statement)) {
    return statement.members.filter(
      (member) =>
        ts.isConstructorDeclaration(member) ||
        ts.isMethodDeclaration(member) ||
        ts.isPropertyDeclaration(member) ||
        ts.isGetAccessorDeclaration(member) ||
        ts.isSetAccessorDeclaration(member),
    )
  }
  if (ts.isInterfaceDeclaration(statement)) {
    return statement.members.filter(
      (member) => ts.isPropertySignature(member) || ts.isMethodSignature(member),
    )
  }
  return []
}

/** Returns a printable name for a declaration node. */
function nameOf(node: ts.Node, source: ts.SourceFile): string {
  if (ts.isConstructorDeclaration(node)) {
    return 'constructor'
  }
  const name = (node as { name?: ts.Node }).name
  return name === undefined ? 'declaration' : name.getText(source)
}

/**
 * Finds comments located inside function, method or arrow function bodies.
 * Tool directives such as `// @ts-expect-error` and `// oxlint-disable` are allowed.
 *
 * @param file - Path used in the reported violations; `.tsx` selects the JSX dialect.
 * @param text - Source text to analyze.
 */
export function findCommentsInsideBodies(file: string, text: string): Violation[] {
  const source = parseSource(file, text)
  const bodies = collectBodies(source)

  return collectComments(source)
    .filter((comment) => !toolDirective.test(text.slice(comment.pos, comment.end)))
    .filter((comment) => bodies.some((body) => comment.pos >= body.start && comment.end <= body.end))
    .map((comment) => violationAt(source, comment.pos, 'comment inside a function body'))
}

/**
 * Finds declarations that lack a TSDoc comment.
 *
 * Checked: functions, classes, interfaces, type aliases and enums; class members and
 * interface members; exported constants; constants holding functions; and, in test files,
 * every top-level `describe` or `test.describe` suite.
 *
 * @param file - Path used in the reported violations; `.tsx` selects the JSX dialect.
 * @param text - Source text to analyze.
 */
export function findUndocumentedDeclarations(file: string, text: string): Violation[] {
  const source = parseSource(file, text)
  const isTestFile = /\.(test|spec)\.tsx?$/.test(file)
  const violations: Violation[] = []

  const require = (node: ts.Node, description: string) => {
    if (!hasDoc(node)) {
      violations.push(violationAt(source, node.getStart(source), `${description} has no TSDoc comment`))
    }
  }

  for (const statement of source.statements) {
    if (
      ts.isFunctionDeclaration(statement) ||
      ts.isClassDeclaration(statement) ||
      ts.isInterfaceDeclaration(statement) ||
      ts.isTypeAliasDeclaration(statement) ||
      ts.isEnumDeclaration(statement)
    ) {
      require(statement, `declaration ${statement.name?.text ?? 'default'}`)
      for (const member of documentedMembers(statement)) {
        require(member, `member ${nameOf(member, source)}`)
      }
    } else if (ts.isVariableStatement(statement)) {
      const declarations = statement.declarationList.declarations
      const callable = declarations.some((declaration) => isCallableInitializer(declaration.initializer))
      if (isExported(statement) || callable) {
        require(statement, `constant ${declarations.map((d) => nameOf(d, source)).join(', ')}`)
      }
    } else if (isTestFile && isSuite(statement)) {
      require(statement, 'test suite')
    }
  }
  return violations
}
