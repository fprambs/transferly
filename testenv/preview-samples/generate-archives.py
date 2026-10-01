"""Regenerates the archive samples of the file viewers inside the tools container, which has 7-Zip and zstd:

    docker compose -f compose.dev.yaml run --rm tools python3 testenv/preview-samples/generate-archives.py

The samples are built from the other sample folders, so each entry opens with a viewer. RAR is written by hand, as RAR5 with the
stored method, because no free tool creates RAR archives.
"""
import io
import os
import shutil
import struct
import subprocess
import tarfile
import tempfile
import zipfile
import zlib

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "archives")
STAMP = 1_790_000_000
PROJECT = {
    "project/README.md": "markdown/README.md",
    "project/src/code.go": "text/code.go",
    "project/src/report.py": "text/report.py",
    "project/data/people.csv": "data/people.csv",
    "project/data/config.yaml": "data/config.yaml",
    "project/images/sample.png": "images/sample.png",
    "project/images/logo.svg": "images/logo.svg",
}


def read(sample):
    """Reads a sample of another folder."""
    with open(os.path.join(HERE, sample), "rb") as f:
        return f.read()


def inner_zip():
    """Builds the small ZIP nested inside the project, to try archives inside archives."""
    buffer = io.BytesIO()
    with zipfile.ZipFile(buffer, "w", zipfile.ZIP_DEFLATED) as z:
        z.writestr(zipfile.ZipInfo("notes/plain.txt", (2026, 9, 30, 12, 0, 0)), read("text/plain.txt"))
        z.writestr(zipfile.ZipInfo("notes/feed.xml", (2026, 9, 30, 12, 0, 0)), read("data/feed.xml"))
    return buffer.getvalue()


def project_files():
    """Lists the files of the project archives, with their contents."""
    files = {name: read(sample) for name, sample in PROJECT.items()}
    files["project/vendor/inner.zip"] = inner_zip()
    return files


def write_tree(root, files):
    """Writes the project to a folder, with fixed times, for the command-line archivers."""
    for name, data in files.items():
        target = os.path.join(root, name)
        os.makedirs(os.path.dirname(target), exist_ok=True)
        with open(target, "wb") as f:
            f.write(data)
        os.utime(target, (STAMP, STAMP))


def write_zip(path, files):
    """Writes a deflated ZIP with an entry for each folder."""
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as z:
        folders = sorted({os.path.dirname(name) + "/" for name in files})
        for folder in folders:
            z.writestr(zipfile.ZipInfo(folder, (2026, 9, 30, 12, 0, 0)), b"")
        for name, data in files.items():
            info = zipfile.ZipInfo(name, (2026, 9, 30, 12, 0, 0))
            info.compress_type = zipfile.ZIP_DEFLATED
            z.writestr(info, data)


def write_tars(files):
    """Writes the project as a plain TAR and as TARs compressed with gzip, bzip2, xz and zstd."""
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode="w", format=tarfile.USTAR_FORMAT) as t:
        for folder in sorted({os.path.dirname(name) for name in files}):
            info = tarfile.TarInfo(folder)
            info.type, info.mode, info.mtime = tarfile.DIRTYPE, 0o755, STAMP
            t.addfile(info)
        for name, data in files.items():
            info = tarfile.TarInfo(name)
            info.size, info.mode, info.mtime = len(data), 0o644, STAMP
            t.addfile(info, io.BytesIO(data))
        link = tarfile.TarInfo("project/LATEST")
        link.type, link.linkname, link.mtime = tarfile.SYMTYPE, "README.md", STAMP
        t.addfile(link)
    plain = buffer.getvalue()
    with open(os.path.join(OUT, "project.tar"), "wb") as f:
        f.write(plain)
    for mode, suffix in (("w:gz", "gz"), ("w:bz2", "bz2"), ("w:xz", "xz")):
        with tarfile.open(os.path.join(OUT, f"project.tar.{suffix}"), mode) as t, tarfile.open(fileobj=io.BytesIO(plain)) as source:
            for member in source.getmembers():
                t.addfile(member, source.extractfile(member) if member.isfile() else None)
    subprocess.run(["zstd", "-q", "-f", "-19", os.path.join(OUT, "project.tar"), "-o", os.path.join(OUT, "project.tar.zst")], check=True)


def write_singles():
    """Writes a log compressed alone with gzip, bzip2, xz and zstd."""
    log = os.path.join(OUT, "server.log")
    shutil.copy(os.path.join(HERE, "text/server.log"), log)
    for tool, suffix in (("gzip", "gz"), ("bzip2", "bz2"), ("xz", "xz"), ("zstd", "zst")):
        with open(f"{log}.{suffix}", "wb") as f:
            subprocess.run([tool, "-c", "-q", log], check=True, stdout=f)
    os.remove(log)


def seven_zip(root, archive, *flags):
    """Writes a 7z archive of the project folder with 7-Zip."""
    target = os.path.join(OUT, archive)
    if os.path.exists(target):
        os.remove(target)
    subprocess.run(["7z", "a", "-bso0", "-bsp0", *flags, target, "project"], check=True, cwd=root)


def vint(value):
    """Encodes a RAR5 variable-length integer."""
    out = bytearray()
    while True:
        byte = value & 0x7F
        value >>= 7
        if value:
            out.append(byte | 0x80)
        else:
            out.append(byte)
            return bytes(out)


def rar_block(kind, flags, fields, data_size=None):
    """Encodes a RAR5 header block: its CRC32, size, type, flags, optional data size and fields."""
    body = vint(kind) + vint(flags) + (vint(data_size) if data_size is not None else b"") + fields
    sized = vint(len(body)) + body
    return struct.pack("<I", zlib.crc32(sized)) + sized


def write_rar(path, files):
    """Writes a RAR5 archive with the stored method: a main header, a file header and data per entry, and the end block."""
    out = bytearray(b"Rar!\x1a\x07\x01\x00")
    out += rar_block(1, 0, vint(0))
    for name, data in files.items():
        encoded = name.encode()
        fields = vint(0x02 | 0x04) + vint(len(data)) + vint(0o644) + struct.pack("<II", STAMP, zlib.crc32(data))
        fields += vint(0) + vint(1) + vint(len(encoded)) + encoded
        out += rar_block(2, 0x02, fields, data_size=len(data)) + data
    out += rar_block(5, 0, vint(0))
    with open(path, "wb") as f:
        f.write(out)


def write_hostile():
    """Writes the archives the viewer must refuse or show safely."""
    with zipfile.ZipFile(os.path.join(OUT, "bomb.zip"), "w", zipfile.ZIP_DEFLATED, compresslevel=9) as z:
        zeros = zipfile.ZipInfo("zeros.bin", (2026, 9, 30, 12, 0, 0))
        zeros.compress_type = zipfile.ZIP_DEFLATED
        with z.open(zeros, "w", force_zip64=True) as entry:
            block = bytes(1 << 20)
            for _ in range(300):
                entry.write(block)
    with zipfile.ZipFile(os.path.join(OUT, "hostile-paths.zip"), "w") as z:
        for name in ("../../../etc/passwd", "/etc/shadow", "C:\\Windows\\win.ini", "a/../../escape.txt", "<img src=x onerror=alert(1)>.txt"):
            z.writestr(zipfile.ZipInfo(name, (2026, 9, 30, 12, 0, 0)), b"a hostile name\n")
    with zipfile.ZipFile(os.path.join(OUT, "many-entries.zip"), "w", zipfile.ZIP_STORED) as z:
        for i in range(12_000):
            z.writestr(zipfile.ZipInfo(f"files/{i:05}.txt", (2026, 9, 30, 12, 0, 0)), b"")
    with open(os.path.join(OUT, "project.zip"), "rb") as f:
        whole = f.read()
    with open(os.path.join(OUT, "truncated.zip"), "wb") as f:
        f.write(whole[: len(whole) // 2])


def main():
    """Writes every archive sample."""
    os.makedirs(OUT, exist_ok=True)
    files = project_files()
    write_zip(os.path.join(OUT, "project.zip"), files)
    write_tars(files)
    write_singles()
    write_rar(os.path.join(OUT, "project.rar"), files)
    with tempfile.TemporaryDirectory() as root:
        write_tree(root, files)
        seven_zip(root, "project.7z", "-mx=9")
        seven_zip(root, "secret.7z", "-ptransferly", "-mhe=on")
        seven_zip(root, "secret-entries.7z", "-ptransferly", "-mhe=off")
        subprocess.run(["7z", "a", "-bso0", "-bsp0", "-tzip", "-ptransferly", os.path.join(OUT, "secret.zip"), "project/README.md"], check=True, cwd=root)
    write_hostile()


if __name__ == "__main__":
    main()
