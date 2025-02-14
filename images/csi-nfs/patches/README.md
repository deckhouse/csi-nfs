## Patches

### Fix go.mod

It fixes https://avd.aquasec.com/nvd/2024/cve-2024-5321/
MUST BE removed after switching to v4.9.0

### nfs-wipe
    
Use with care: this will probably slow down removal process

Also it's not totally crypto-safe (because of modern FS features, such as
journaling, CoW).
This also doesn't consider specific storage features
(such as, TRIM/flash memory controller internal algorithms, etc).

`shred` from GNU coreutils is currently used as a wiping backend.
TODO: switch to srm from secure-delete package and/or self-coded solution.

Please also see papers:
* "Secure Deletion of Data from Magnetic and Solid-State Memory" by Peter Gutmann

    https://www.cs.auckland.ac.nz/~pgut001/pubs/secure_del.html
* "Reliably Erasing Data From Flash-Based Solid State Drives" by Michael Wei et. al.

    https://www.usenix.org/legacy/events/fast11/tech/full_papers/Wei.pdf

