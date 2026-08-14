# iTop 3.2.3-2 auf PHP 8.3 (Debian bookworm)
#
# Bauen:
#   1. Offizielles iTop-ZIP nach ./iTop-3.2.3-2-20678.zip legen (GitHub-Release)
#   2. docker build -t itop:3.2.3-2-php8.3 /opt/itop-test/docker
#
# Dieser Host (itop.example.internal) hat direkten Internetzugang, deshalb werden hier
# keine Proxy-Build-Args uebergeben. Die Proxy-Logik bleibt drin, damit das
# Dockerfile mit der Vorlage identisch bleibt und in der Proxy-Umgebung ohne
# Aenderung funktioniert - ohne Argument passiert schlicht nichts.
#
# PHP-Version: 8.3 ist der konservative Zielwert.
#   iTop 3.2.0   akzeptiert 8.1.0 bis <8.4.0   (PHP_NOT_VALIDATED_VERSION = 8.4.0)
#   iTop 3.2.3-2 akzeptiert 8.1.0 bis <8.5.0   (PHP_NOT_VALIDATED_VERSION = 8.5.0)
# 8.3 ist damit in beiden Versionen freigegeben - der Rollback auf 3.2.0 bleibt moeglich.
# Wer 8.4 will, kann FROM aendern; das ist erst ab iTop 3.2.3 validiert.

FROM php:8.3-apache

# ---------------------------------------------------------------------------
# Proxy
#
# Docker reicht HTTP_PROXY/http_proxy als vordefinierte Build-Args durch, aber
# nur in genau der Schreibweise, die uebergeben wurde. apt und pear lesen die
# KLEINSCHREIBUNG. Statt sich darauf zu verlassen, wird der Proxy hier explizit
# in die apt- und pear-Konfiguration geschrieben - dann ist es egal, welche
# Variante beim Build-Aufruf gesetzt war.
#
# Ohne Proxy-Argument passiert nichts; das Dockerfile bleibt auch ohne Proxy
# benutzbar.
# ---------------------------------------------------------------------------
ARG http_proxy
ARG https_proxy
ARG HTTP_PROXY
ARG HTTPS_PROXY

RUN set -eux; \
    PROXY="${http_proxy:-${HTTP_PROXY:-}}"; \
    PROXYS="${https_proxy:-${HTTPS_PROXY:-$PROXY}}"; \
    if [ -n "$PROXY" ]; then \
        printf 'Acquire::http::Proxy "%s";\nAcquire::https::Proxy "%s";\n' "$PROXY" "$PROXYS" \
            > /etc/apt/apt.conf.d/01proxy; \
        pear config-set http_proxy "$PROXY" || true; \
        echo "Proxy fuer apt und pear gesetzt: $PROXY"; \
    else \
        echo "Kein Proxy uebergeben - direkte Verbindung wird versucht."; \
    fi

# ---------------------------------------------------------------------------
# Optionale Build-Zutaten aus ./build-assets/
#
#   *.crt   -> werden als vertrauenswuerdige CAs installiert
#
# Hintergrund: In der Ursprungsumgebung bricht der Proxy TLS auf und ersetzt
# Serverzertifikate. apt merkt davon nichts (Debian-Repos laufen ueber http),
# pecl schon - es scheitert mit "certificate verify failed" in PEAR/Proxy.php.
#
# Auf diesem Host ohne Proxy ist das Verzeichnis leer (.keep). Es muss aber
# existieren: COPY eines nicht vorhandenen Pfades bricht den Build ab.
# ---------------------------------------------------------------------------
COPY build-assets/ /tmp/assets/

RUN set -eux; \
    if ls /tmp/assets/*.crt >/dev/null 2>&1; then \
        cp /tmp/assets/*.crt /usr/local/share/ca-certificates/; \
        update-ca-certificates; \
        echo "==> CA-Zertifikate installiert:"; ls -1 /tmp/assets/*.crt; \
    else \
        echo "==> keine CA-Zertifikate im Build-Kontext"; \
    fi

# Offizielles Release-Asset:
#   https://github.com/Combodo/iTop/releases/download/3.2.3-2/iTop-3.2.3-2-20678.zip
#   32206630 Bytes
#   sha256 4ecb62d0d7d2205fca394bc59c6858e2e85e51025c28f51d07df15ff9ea20a82
# Archivlayout: web/ , INSTALL , LICENSE  -> iTop liegt unter web/
ARG ITOP_ZIP=iTop-3.2.3-2-20678.zip

# ---------------------------------------------------------------------------
# PHP-Erweiterungen
#
# Pflicht laut SetupUtils::GetPHPMandatoryExtensions() (iTop 3.2.3-2, Zeile 2095):
#   mysqli iconv simplexml soap hash json session pcre dom zlib zip
#   fileinfo mbstring gd curl
# Davon sind im offiziellen php-Image bereits enthalten:
#   iconv simplexml hash json session pcre dom zlib fileinfo mbstring curl
# Nachzuinstallieren bleiben: mysqli soap zip gd
#
# Optional laut GetPHPOptionalExtensions() (Zeile 2119):
#   mcrypt|sodium|openssl -> sodium und openssl sind im Basis-Image enthalten
#   ldap                  -> installiert
#   apcu                  -> hier INSTALLIERT
#
# Abweichung zur Vorlage: dort wurde apcu weggelassen, weil pecl ueber https
# laedt und am TLS-aufbrechenden Proxy scheiterte. Dieser Host hat direkten
# Internetzugang, das Hindernis entfaellt - also wird apcu mitgenommen. iTop
# stuft es als optional ein und meldet ohne apcu im Setup-Check die Warnung
# "Performances will be slightly degraded."
# ---------------------------------------------------------------------------
RUN set -eux; \
    savedAptMark="$(apt-mark showmanual)"; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        libxml2-dev \
        libzip-dev \
        libpng-dev \
        libjpeg62-turbo-dev \
        libfreetype-dev \
        libldap2-dev \
    ; \
    docker-php-ext-configure gd --with-freetype --with-jpeg; \
    docker-php-ext-configure ldap --with-libdir="lib/$(dpkg-architecture -q DEB_HOST_MULTIARCH)"; \
    docker-php-ext-install -j"$(nproc)" mysqli soap zip gd ldap opcache; \
    pecl install apcu; \
    docker-php-ext-enable apcu; \
    rm -rf /tmp/assets; \
    \
    # Build-Abhaengigkeiten wieder entfernen, Laufzeit-Libs behalten
    apt-mark auto '.*' > /dev/null; \
    apt-mark manual $savedAptMark > /dev/null; \
    ldd "$(php -r 'echo ini_get("extension_dir");')"/*.so \
        | awk '/=>/ { so = $(NF-1); if (index(so, "/usr/local/") == 1) next; gsub("^/(usr/)?", "", so); printf "*%s\n", so }' \
        | sort -u \
        | xargs -r dpkg-query --search 2>/dev/null \
        | cut -d: -f1 \
        | sort -u \
        | xargs -r apt-mark manual; \
    apt-get purge -y --auto-remove -o APT::AutoRemove::RecommendsImportant=false; \
    rm -rf /var/lib/apt/lists/*

# ---------------------------------------------------------------------------
# Laufzeit-Pakete
#   graphviz:       von SetupUtils::CheckGraphviz() geprueft, ohne dot keine
#                   Impact-Analyse-Grafiken
#   mariadb-client: liefert mysqldump. Ohne das:
#                     - jeder Setup-Upgrade-Lauf bricht in CheckBackupPrerequisites
#                       ab: "mysqldump could not be executed (retcode=127)"
#                     - der Hintergrund-Task BackupExec (taeglich 23:30, vom Modul
#                       itop-backup registriert) scheitert jede Nacht still
#                   Das Paket legt mysqldump als Kompatibilitaets-Symlink auf
#                   mariadb-dump an, genau den Namen sucht iTop.
#
#                   Versions-Schiefstand beachten: Debian bookworm liefert den
#                   11.8er-Client, der Server im Stack ist 12.2. Fuer logische
#                   Dumps unkritisch. Wer das sauber will, nimmt MariaDBs eigenes
#                   apt-Repository und pinnt den Client auf die Serverversion.
# ---------------------------------------------------------------------------
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends graphviz unzip mariadb-client; \
    rm -rf /var/lib/apt/lists/*; \
    # Proxy-Konfiguration nicht ins fertige Image schleppen - sie wuerde in
    # abgeleiteten Builds oder in einer anderen Netzumgebung stoeren.
    rm -f /etc/apt/apt.conf.d/01proxy

# ---------------------------------------------------------------------------
# Apache
#   rewrite: von iTop benoetigt
#   AllowOverride All: iTop liefert .htaccess-Dateien aus, die u.a. den
#                      Direktzugriff auf data/ und conf/ verhindern. Mit dem
#                      Debian-Default AllowOverride None waeren die wirkungslos.
# ---------------------------------------------------------------------------
RUN set -eux; \
    a2enmod rewrite headers; \
    printf 'ServerName itop.local\n' > /etc/apache2/conf-available/servername.conf; \
    a2enconf servername; \
    printf '<Directory /var/www/html>\n\tAllowOverride All\n\tRequire all granted\n</Directory>\n' \
        > /etc/apache2/conf-available/itop.conf; \
    a2enconf itop

# ---------------------------------------------------------------------------
# PHP-Grundeinstellungen
#
# Praefix 90-, damit der per Bind-Mount eingehaengte 99-itop.ini weiterhin
# gewinnt. MIN_MEMORY_LIMIT ist laut Core 32M - 512M ist der praxistaugliche
# Wert fuer Impact-Analyse und Exporte.
# ---------------------------------------------------------------------------
RUN set -eux; \
    { \
        echo 'memory_limit = 512M'; \
        echo 'max_execution_time = 300'; \
        echo 'upload_max_filesize = 32M'; \
        echo 'post_max_size = 64M'; \
        echo 'date.timezone = Europe/Berlin'; \
        echo 'display_errors = Off'; \
        echo 'display_startup_errors = Off'; \
        echo 'log_errors = On'; \
        echo 'opcache.enable = 1'; \
        echo 'opcache.memory_consumption = 192'; \
        echo 'opcache.max_accelerated_files = 20000'; \
        echo 'opcache.validate_timestamps = 1'; \
        echo 'opcache.revalidate_freq = 60'; \
    } > /usr/local/etc/php/conf.d/90-itop-base.ini

# ---------------------------------------------------------------------------
# iTop
#
# Das ZIP wird lokal bereitgestellt statt im Build heruntergeladen: reproduzierbar,
# funktioniert hinter dem Proxy und dokumentiert die exakte Version im Repo.
# Die Verzeichnisstruktur im Archiv wird nicht angenommen, sondern gesucht -
# Combodo hat das Layout zwischen Releases schon geaendert.
# ---------------------------------------------------------------------------
COPY ${ITOP_ZIP} /tmp/itop.zip

RUN set -eux; \
    unzip -q /tmp/itop.zip -d /tmp/itop; \
    ITOP_SRC="$(dirname "$(dirname "$(find /tmp/itop -type f -path '*/setup/wizard.php' | head -1)")")"; \
    test -n "$ITOP_SRC"; \
    test -d "$ITOP_SRC/setup"; \
    rm -rf /var/www/html; \
    mkdir -p /var/www/html; \
    cp -a "$ITOP_SRC/." /var/www/html/; \
    rm -rf /tmp/itop /tmp/itop.zip; \
    # data/backups muss hier mit angelegt werden, obwohl es im Stack ein eigenes
    # Volume ist - gerade deswegen: Docker uebernimmt die Ownership eines neuen
    # Named Volumes vom Mountpunkt IM IMAGE. Existiert der Pfad dort nicht,
    # legt Docker ihn als root an, und der Setup-Wizard scheitert beim ersten
    # mkdir("data/backups/manual") mit "Permission denied".
    mkdir -p /var/www/html/conf /var/www/html/data /var/www/html/data/backups \
             /var/www/html/log /var/www/html/env-production /var/www/html/extensions; \
    chown -R www-data:www-data /var/www/html

# ---------------------------------------------------------------------------
# Extensions
#
# Alles aus dem Build-Kontext-Ordner ./extensions/ wird ins Image gebacken:
#   *.zip       -> nach /var/www/html/extensions/ entpackt
#   Verzeichnis -> unveraendert kopiert
#
# ACHTUNG - damit das ueberhaupt wirkt, darf im Stack KEIN Volume mehr auf
# /var/www/html/extensions/ gemountet sein. Docker kopiert Image-Inhalte nur
# dann in ein Named Volume, wenn dieses LEER ist. Ein nicht leeres Volume
# wuerde die gebackenen Extensions vollstaendig verdecken - ohne Fehlermeldung.
#
# Erwarteter Inhalt von ./extensions/ :
#   teemip-core-ip-mgmt-3.2.2-814.zip            (SourceForge "an iTop module")
#   teemip-network-mgmt-extended-3.2.2-212.zip   (SourceForge "extensions")
#   combodo-saml-1_2_1.zip                       (wird nachgereicht)
#   custom-service-software/                     (Verzeichnis, wird nachgereicht)
#
# Es MUESSEN alle bereits installierten Extensions dabei sein. iTop kann
# installierte Module nicht deinstallieren; verschwindet eines vom Dateisystem,
# bricht der Setup-Lauf ab. Bei dieser Neuinstallation ist noch nichts
# installiert - Nachruesten per Rebuild und erneutem Setup-Lauf ist moeglich.
# ---------------------------------------------------------------------------
COPY extensions/ /tmp/ext/

RUN set -eux; \
    for z in /tmp/ext/*.zip; do \
        [ -e "$z" ] || continue; \
        echo "==> entpacke $(basename "$z")"; \
        unzip -q -o "$z" -d /var/www/html/extensions/; \
    done; \
    for d in /tmp/ext/*/; do \
        [ -d "$d" ] || continue; \
        echo "==> kopiere $(basename "$d")"; \
        cp -a "$d" /var/www/html/extensions/; \
    done; \
    rm -rf /tmp/ext; \
    chown -R www-data:www-data /var/www/html/extensions; \
    echo "--- Inhalt von extensions/ ---"; \
    ls -1 /var/www/html/extensions/

WORKDIR /var/www/html

# ---------------------------------------------------------------------------
# Entrypoint
#
# Korrigiert bei jedem Start die Ownership auf conf/, data/ und
# env-production/. Die haengen an Volumes, und deren Ownership stammt nicht
# aus dem Image - ein frisches Volume oder ein zurueckgespieltes Backup
# bringt Apache sonst mit "Permission denied" beim Twig-Cache zu Fall.
# ---------------------------------------------------------------------------
COPY entrypoint.sh /usr/local/bin/itop-entrypoint.sh
RUN chmod +x /usr/local/bin/itop-entrypoint.sh

ENTRYPOINT ["/usr/local/bin/itop-entrypoint.sh"]
CMD ["apache2-foreground"]
