//! Detects source languages and caches palette roles for their syntax scopes.

use std::sync::LazyLock;

use ratatui::style::Color;
use syntect::parsing::syntax_definition::{Context, MatchOperation, MatchPattern, Pattern};
use syntect::parsing::{
    ParseState, Scope, ScopeStack, SyntaxDefinition, SyntaxReference, SyntaxSet,
};

use crate::ui::theme::Palette;
use crate::ui::widgets::visible_character;

/// Highlight this many lines at most, leaving the rest as plain text.
const LINE_LIMIT: usize = 20_000;

/// The bundled grammars are shared by all previews.
static SYNTAXES: LazyLock<SyntaxSet> = LazyLock::new(load_syntaxes);

/// Adds small grammars for structured formats missing from the bundled set.
fn load_syntaxes() -> SyntaxSet {
    const QUOTED: &str = r#""(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'"#;
    const NUMBER: &str = r"\b[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?\b";
    let mut builder = SyntaxSet::load_defaults_nonewlines().into_builder();
    // An extension lookup for JSON still prefers a bundled grammar when one is available.
    builder.add(line_grammar(
        "JSON",
        "source.transferly-json",
        &[],
        &[
            (r#""(?:\\.|[^"\\])*"(?=\s*:)"#, "entity.name"),
            (r#""(?:\\.|[^"\\])*""#, "string"),
            (r"\b(?:true|false|null)\b", "constant"),
            (NUMBER, "constant.numeric"),
        ],
    ));
    builder.add(line_grammar(
        "Configuration",
        "source.transferly-config",
        &["ini", "conf", "toml", "env"],
        &[
            (r"^\s*\[{1,2}[^\]]+\]{1,2}", "entity.name"),
            (r"^\s*(?:export\s+)?[\w.-]+(?=\s*=)", "keyword"),
            (QUOTED, "string"),
            (r"(?:#.*|^\s*;.*)", "comment"),
            (r"\b(?:true|false|on|off|null)\b", "constant"),
            (NUMBER, "constant.numeric"),
        ],
    ));
    builder.add(line_grammar(
        "Dockerfile",
        "source.transferly-dockerfile",
        &["dockerfile", "containerfile"],
        &[
            (
                r"(?i)^\s*(?:FROM|RUN|CMD|LABEL|MAINTAINER|EXPOSE|ENV|ADD|COPY|ENTRYPOINT|VOLUME|USER|WORKDIR|ARG|ONBUILD|STOPSIGNAL|HEALTHCHECK|SHELL)\b",
                "keyword",
            ),
            (r"^\s*#.*", "comment"),
            (QUOTED, "string"),
            (r"\$(?:\w+|\{[^}]+\})", "entity.name"),
            (NUMBER, "constant.numeric"),
        ],
    ));
    builder.add(line_grammar(
        "Log",
        "text.transferly-log",
        &["log"],
        &[
            (
                r"(?i)\b(?:warn(?:ing)?|error|fatal|panic|critical)\b",
                "constant",
            ),
            (r"(?i)\b(?:trace|debug|info|notice)\b", "keyword"),
            (QUOTED, "string"),
            (
                r"\b\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}:\d{2})?",
                "entity.name",
            ),
        ],
    ));
    builder.build()
}

/// Builds a syntect grammar whose rules annotate independent lines.
fn line_grammar(
    name: &str,
    scope: &str,
    extensions: &[&str],
    rules: &[(&str, &str)],
) -> SyntaxDefinition {
    let scope = Scope::new(scope).expect("a source scope");
    let mut context = Context::new(false);
    context.meta_content_scope.push(scope);
    context
        .patterns
        .extend(rules.iter().map(|&(pattern, scope)| {
            Pattern::Match(MatchPattern::new(
                false,
                pattern.to_owned(),
                vec![Scope::new(scope).expect("a token scope")],
                None,
                MatchOperation::None,
                None,
            ))
        }));
    SyntaxDefinition {
        name: name.to_owned(),
        file_extensions: extensions
            .iter()
            .map(|&extension| extension.to_owned())
            .collect(),
        scope,
        first_line_match: None,
        hidden: false,
        variables: std::collections::HashMap::default(),
        contexts: [("__start".to_owned(), context)].into(),
    }
}

/// A palette role associated with a syntax scope.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(super) enum Role {
    /// Ordinary source text.
    Text,
    /// Comments and documentation.
    Comment,
    /// Control flow and declarations.
    Keyword,
    /// Quoted literals.
    String,
    /// Numbers and other constants.
    Constant,
    /// Functions, tags and types.
    Symbol,
}

impl Role {
    /// Resolves the role in the currently selected theme.
    pub(super) fn color(self, palette: &Palette) -> Color {
        match self {
            Self::Text => palette.text,
            Self::Comment => palette.muted,
            Self::Keyword => palette.accent,
            Self::String => palette.ok,
            Self::Constant => palette.warn,
            Self::Symbol => palette.info,
        }
    }
}

/// Scope prefixes in the bundled grammars that carry a palette role.
static ROLES: LazyLock<[(Scope, Role); 10]> = LazyLock::new(|| {
    [
        ("comment", Role::Comment),
        ("keyword", Role::Keyword),
        ("storage.type", Role::Keyword),
        ("storage.modifier", Role::Keyword),
        ("string", Role::String),
        ("constant", Role::Constant),
        ("entity.name", Role::Symbol),
        ("support.function", Role::Symbol),
        ("support.type", Role::Symbol),
        ("support.class", Role::Symbol),
    ]
    .map(|(scope, role)| (Scope::new(scope).expect("a scope prefix"), role))
});

/// The byte offset where a palette role starts on a line.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(super) struct Token {
    /// The beginning of the run, in UTF-8 bytes.
    pub(super) start: usize,
    /// The color role to use until the next token.
    pub(super) role: Role,
}

/// A display label and the grammar used for it.
pub(super) struct Language {
    /// The language name shown in the preview title.
    pub(super) name: &'static str,
    /// The grammar used to recognize scopes.
    grammar: &'static SyntaxReference,
}

impl Language {
    /// Resolves a grammar by its bundled extension or name.
    fn named(token: &str) -> Option<Self> {
        let grammar = SYNTAXES.find_syntax_by_token(token)?;
        Some(Self {
            name: &grammar.name,
            grammar,
        })
    }

    /// Prefers the exact syntax and uses a related grammar when the bundle lacks it.
    fn related(name: &'static str, token: &str, fallback: &str) -> Option<Self> {
        SYNTAXES
            .find_syntax_by_token(token)
            .or_else(|| SYNTAXES.find_syntax_by_token(fallback))
            .map(|grammar| Self { name, grammar })
    }

    /// Finds a grammar from a file extension or conventional basename.
    pub(super) fn for_file(name: &str, extension: &str) -> Option<Self> {
        let basename = name.to_ascii_lowercase();
        match (basename.as_str(), extension) {
            ("dockerfile" | "containerfile", _) | (_, "dockerfile" | "containerfile") => {
                Self::named("dockerfile")
            }
            ("makefile" | "gnumakefile", _) => Self::named("Makefile"),
            (".env", _) | (_, "env") => Self::related("dotenv", "env", "ini"),
            (_, "bash" | "zsh") => Self::named("sh"),
            (_, "tsx" | "ts") => Self::related("TypeScript", extension, "js"),
            (_, "jsx") => Self::related("JavaScript (JSX)", "jsx", "js"),
            (_, "kt" | "kts") => Self::related("Kotlin", extension, "java"),
            (_, "toml") => Self::related("TOML", "toml", "ini"),
            (_, "ini") => Self::related("INI", "ini", "conf"),
            (_, "svg") => Self::named("xml"),
            (_, "") => None,
            _ => Self::named(extension).filter(|language| language.name != "Plain Text"),
        }
    }

    /// Looks for a first-line directive or a structured text signature.
    pub(super) fn for_content(text: &str) -> Option<Self> {
        let first = text.lines().next().unwrap_or_default();
        let trimmed = text.trim_start();
        if trimmed.starts_with("<?xml") {
            Self::named("xml")
        } else if trimmed
            .lines()
            .next()
            .is_some_and(|line| line.trim_end() == "---")
        {
            Self::named("yaml")
        } else if trimmed
            .get(..14)
            .is_some_and(|prefix| prefix.eq_ignore_ascii_case("<!doctype html"))
        {
            Self::named("html")
        } else {
            SYNTAXES
                .find_syntax_by_first_line(first)
                .map(|grammar| Self {
                    name: &grammar.name,
                    grammar,
                })
        }
    }

    /// Stores syntax transitions once, carrying the parser state across lines.
    pub(super) fn highlight(&self, text: &str) -> Vec<Vec<Token>> {
        let mut state = ParseState::new(self.grammar);
        let mut scopes = ScopeStack::new();
        text.lines()
            .take(LINE_LIMIT)
            .map(|line| {
                let operations = state.parse_line(line, &SYNTAXES).unwrap_or_default();
                let mut tokens = Vec::new();
                let mut previous = active_role(&scopes);
                tokens.push(Token {
                    start: 0,
                    role: previous,
                });
                for (start, operation) in operations {
                    let _ = scopes.apply(&operation);
                    let role = active_role(&scopes);
                    if role != previous {
                        tokens.push(Token { start, role });
                        previous = role;
                    }
                }
                if line.chars().any(char::is_control) {
                    visible_offsets(line, &mut tokens);
                }
                tokens
            })
            .collect()
    }
}

/// Chooses the most specific recognized role in the current scope stack.
fn active_role(scopes: &ScopeStack) -> Role {
    scopes
        .as_slice()
        .iter()
        .rev()
        .find_map(|&scope| {
            ROLES
                .iter()
                .find_map(|&(prefix, role)| prefix.is_prefix_of(scope).then_some(role))
        })
        .unwrap_or(Role::Text)
}

/// Accounts for the wider display glyphs used in place of control characters.
fn visible_offsets(line: &str, tokens: &mut [Token]) {
    let mut characters = line.char_indices().peekable();
    let mut width = 0;
    for token in tokens {
        while let Some((_, character)) = characters.next_if(|(offset, _)| *offset < token.start) {
            width += visible_character(character).len_utf8();
        }
        token.start = width;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Extensions and conventional names select a source grammar before content detection.
    #[test]
    fn detects_named_languages() {
        for (name, extension) in [
            ("main.rs", "rs"),
            ("main.go", "go"),
            ("main.py", "py"),
            ("main.js", "js"),
            ("main.ts", "ts"),
            ("main.tsx", "tsx"),
            ("main.jsx", "jsx"),
            ("data.json", "json"),
            ("data.yml", "yml"),
            ("data.toml", "toml"),
            ("image.svg", "svg"),
            ("style.css", "css"),
            ("script.sh", "sh"),
            ("script.bash", "bash"),
            ("script.zsh", "zsh"),
            ("query.sql", "sql"),
            ("readme.md", "md"),
            ("main.java", "java"),
            ("main.kt", "kt"),
            ("main.c", "c"),
            ("main.h", "h"),
            ("main.cpp", "cpp"),
            ("main.cs", "cs"),
            ("main.rb", "rb"),
            ("main.php", "php"),
            ("Dockerfile", ""),
            ("Makefile", "mk"),
            (".env", ""),
            ("app.conf", "conf"),
            ("app.ini", "ini"),
            ("app.log", "log"),
        ] {
            assert!(Language::for_file(name, extension).is_some(), "{name}");
        }
        assert!(Language::for_file("unrecognized.filetype", "filetype").is_none());
    }

    /// Content signatures identify scripts, XML, YAML and HTML without extensions.
    #[test]
    fn detects_first_line_signatures() {
        for (text, extension) in [
            ("#!/usr/bin/env python\nprint('hello')", "py"),
            ("  <?xml version=\"1.0\"?><item />", "xml"),
            ("---\nname: reader\n---", "yaml"),
            ("<!DOCTYPE html>\n<html></html>", "html"),
        ] {
            let language = Language::for_content(text).expect(text);
            assert_eq!(
                language.name,
                Language::for_file("", extension).expect(extension).name
            );
        }
        assert!(Language::for_content("plain text").is_none());
    }

    /// Highlights syntax specific to the supplemental structured formats.
    #[test]
    fn highlights_structured_formats() {
        for (name, extension, text, start, role) in [
            ("Dockerfile", "", "FROM alpine", 0, Role::Keyword),
            ("app.ini", "ini", "[section]", 0, Role::Symbol),
            ("app.toml", "toml", "value = true", 8, Role::Constant),
            (".env", "", "VALUE=\"text\"", 6, Role::String),
            (
                "server.log",
                "log",
                "ERROR: connection closed",
                0,
                Role::Constant,
            ),
        ] {
            let language = Language::for_file(name, extension).expect(name);
            assert!(
                language.highlight(text)[0].contains(&Token { start, role }),
                "{name}"
            );
        }
    }

    /// Keeps multiline comment scopes and translates offsets past a displayed tab.
    #[test]
    fn caches_roles_up_to_the_line_limit() {
        let language = Language::for_file("main.rs", "rs").expect("Rust");
        let source = format!(
            "/* comment\nstill a comment */\n\tfn main() {{}}\n{}",
            "\n".repeat(LINE_LIMIT)
        );
        let highlighted = language.highlight(&source);
        assert_eq!(highlighted.len(), LINE_LIMIT);
        assert_eq!(highlighted[1][0].role, Role::Comment);
        assert!(highlighted[2].contains(&Token {
            start: '⇥'.len_utf8(),
            role: Role::Keyword,
        }));
    }
}
