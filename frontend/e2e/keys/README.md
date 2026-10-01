# Test keys

A PuTTY key (PPK 3, RSA 2048, Argon2id, passphrase `correct horse`) and its public key, made for the tests and used nowhere else. The isolated SFTP server of `compose.test.yaml` authorizes the public key for the user `transferly`, so the e2e run can save a site with the key file and connect with it.
