//! The languages of the interface, English and Spanish, as the web has them: before signing in the language of the system, then the
//! language of the account. The messages live in `tui/locales/<language>.json`, embedded in the binary.

use serde_json::Value;

/// A language of the interface.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Language {
    /// English, the language messages are written in first.
    English,
    /// Spanish.
    Spanish,
}

impl Language {
    /// The language of a BCP 47 tag or a locale such as `es_CL.UTF-8`; anything that is not Spanish is English.
    #[must_use]
    pub fn from_tag(tag: &str) -> Self {
        if tag.to_ascii_lowercase().starts_with("es") {
            Self::Spanish
        } else {
            Self::English
        }
    }

    /// The language of the system, from the first of `LC_ALL`, `LC_MESSAGES` and `LANG` that is set.
    #[must_use]
    pub fn from_env(env: impl Fn(&str) -> Option<String>) -> Self {
        ["LC_ALL", "LC_MESSAGES", "LANG"]
            .iter()
            .find_map(|name| env(name).filter(|value| !value.is_empty()))
            .map_or(Self::English, |value| Self::from_tag(&value))
    }

    /// The tag of the language, as the account stores it.
    #[must_use]
    pub fn tag(self) -> &'static str {
        match self {
            Self::English => "en",
            Self::Spanish => "es",
        }
    }
}

/// The messages of one language.
#[derive(Debug, Clone)]
pub struct Messages {
    language: Language,
    tree: Value,
}

impl Messages {
    /// The messages of a language.
    ///
    /// # Panics
    ///
    /// A catalog embedded in the binary is not valid JSON, which its tests rule out.
    #[must_use]
    pub fn new(language: Language) -> Self {
        let text = match language {
            Language::English => include_str!("../locales/en.json"),
            Language::Spanish => include_str!("../locales/es.json"),
        };
        Self {
            language,
            tree: serde_json::from_str(text).expect("an embedded catalog"),
        }
    }

    /// The language of these messages.
    #[must_use]
    pub fn language(&self) -> Language {
        self.language
    }

    /// The message at a dotted key, such as `server.title`; a missing key shows itself, so it is noticed.
    #[must_use]
    pub fn get(&self, key: &str) -> String {
        key.split('.')
            .try_fold(&self.tree, |node, part| node.get(part))
            .and_then(Value::as_str)
            .unwrap_or(key)
            .to_owned()
    }

    /// The message at a key with its `{name}` placeholders filled.
    #[must_use]
    pub fn fill(&self, key: &str, values: &[(&str, &str)]) -> String {
        values.iter().fold(self.get(key), |text, (name, value)| {
            text.replace(&format!("{{{name}}}"), value)
        })
    }
}

#[cfg(test)]
mod tests {
    use std::collections::BTreeSet;

    use super::*;

    /// Every dotted key of a catalog, with whether its placeholders are the same in every language.
    fn keys(node: &Value, prefix: &str, out: &mut BTreeSet<String>) {
        match node {
            Value::Object(map) => {
                for (name, child) in map {
                    keys(child, &format!("{prefix}{name}."), out);
                }
            }
            _ => {
                out.insert(prefix.trim_end_matches('.').to_owned());
            }
        }
    }

    /// English and Spanish have the same keys, every message is text, and each Spanish message has the placeholders of the English one.
    #[test]
    fn catalogs_match() {
        let english = Messages::new(Language::English);
        let spanish = Messages::new(Language::Spanish);
        let (mut en, mut es) = (BTreeSet::new(), BTreeSet::new());
        keys(&english.tree, "", &mut en);
        keys(&spanish.tree, "", &mut es);
        assert_eq!(en, es);
        for key in &en {
            let placeholders = |text: String| {
                text.split('{')
                    .skip(1)
                    .map(|part| part.split('}').next().unwrap_or("").to_owned())
                    .collect::<BTreeSet<_>>()
            };
            assert_eq!(
                placeholders(english.get(key)),
                placeholders(spanish.get(key)),
                "{key}"
            );
            assert_ne!(english.get(key), *key, "{key} is not text");
        }
    }

    /// Messages are found by key and filled; a missing key shows itself.
    #[test]
    fn finds_and_fills_messages() {
        let spanish = Messages::new(Language::Spanish);
        assert_eq!(spanish.language(), Language::Spanish);
        assert_eq!(spanish.get("server.title"), "Iniciar sesión");
        assert_eq!(
            spanish.fill(
                "signed.body",
                &[("server", "https://x"), ("email", "ana@x")]
            ),
            "Sesión iniciada en https://x como ana@x."
        );
        assert_eq!(spanish.get("server.nothing"), "server.nothing");
        assert_eq!(spanish.get("server"), "server");
    }

    /// The language comes from the tag or the environment, Spanish for any Spanish locale and English otherwise.
    #[test]
    fn picks_the_language() {
        assert_eq!(Language::from_tag("es_CL.UTF-8"), Language::Spanish);
        assert_eq!(Language::from_tag("ES"), Language::Spanish);
        assert_eq!(Language::from_tag("en-US"), Language::English);
        let env = |pairs: &'static [(&'static str, &'static str)]| {
            move |name: &str| {
                pairs
                    .iter()
                    .find(|(k, _)| *k == name)
                    .map(|(_, v)| (*v).to_owned())
            }
        };
        assert_eq!(
            Language::from_env(env(&[("LANG", "es_ES.UTF-8")])),
            Language::Spanish
        );
        assert_eq!(
            Language::from_env(env(&[("LC_ALL", ""), ("LANG", "es_ES")])),
            Language::Spanish
        );
        assert_eq!(
            Language::from_env(env(&[("LC_ALL", "C"), ("LANG", "es_ES")])),
            Language::English
        );
        assert_eq!(Language::from_env(env(&[])), Language::English);
        assert_eq!(
            (Language::English.tag(), Language::Spanish.tag()),
            ("en", "es")
        );
    }
}
