# Informe — TP0 Nivelador

Sistema distribuido de lotería nacional: N agencias (clientes en Go) cargan apuestas en un
servidor central (servidor en Python), que las almacena, realiza el sorteo una vez alcanzado un
mínimo de agencias y devuelve a cada una únicamente sus ganadores.

Este informe detalla los aspectos más importantes de la solución, con foco en el **protocolo de comunicación** y los **mecanismos de sincronización** de la ejecución concurrente.

---

## 1. Cómo levantar y correr el sistema

```bash
make up      # construye las imágenes y levanta los contenedores
make logs    # sigue los logs de todos los contenedores
make down    # detiene y elimina los contenedores
make test    # pruebas provistas
```

Toda la configuración llega por variables de entorno declaradas en `docker-compose.yaml`. El servidor toma `SERVER_HOST`, `SERVER_PORT` y `AGENCY_QUORUM_MIN`; cada cliente toma `AGENCY_ID`, `SERVER_HOST`, `SERVER_PORT`, `INPUT_FILE`, `OUTPUT_FILE` y
`BATCH_SIZE`. 
La ausencia de cualquiera de ellas, o un valor inválido, es un error de arranque
explícito y reportado, no una falla a mitad de la ejecución. `BATCH_SIZE` es política del cliente y
`AGENCY_QUORUM_MIN` es política del servidor, y por eso cada proceso declara solo las suyas.

---

## 2. Organización del código

La responsabilidad quedó repartida en tres capas en los dos lenguajes:
- Un paquete de **dominio** que define la apuesta y sabe interpretar un registro del archivo
- Un paquete de **protocolo** que traduce entre esa apuesta y los bytes del cable
- Una capa de **orquestación** que se limita a encadenar los pasos

El paquete de protocolo es el único que utiliza las funciones de socket seguro, con lo cual el resto del programa nunca manipula el socket directamente.

En el servidor, la deserialización es el punto exacto donde la capa de comunicación construye el modelo
de dominio: los enteros del cable se convierten en los enteros de la apuesta, y la agencia la
aporta la conexión, no el paquete.

---

## 3. Protocolo de comunicación

### 3.1. Formato de los mensajes

Todo mensaje viaja con la misma envoltura: **un byte de tipo, el largo del payload en cuatro bytes
big-endian, y el payload**.

El largo va por delante porque TCP entrega un flujo de bytes sin fronteras de mensaje: conociendo
de antemano cuántos bytes faltan, el receptor puede apoyarse en las funciones de lectura segura,
cuya condición de corte es un contador. La alternativa —delimitar los mensajes con un separador—
obligaría a leer de a un byte buscándolo y además a escaparlo si apareciera dentro de un nombre.

El byte de tipo hace el protocolo autodescriptivo: un mensaje que llega fuera de lugar se rechaza
como violación del protocolo en lugar de interpretarse como datos. Los enteros viajan en
big-endian, de modo que el formato no dependa del *endianness* de ninguno de los dos extremos.

### 3.2. Los mensajes y el flujo de una conexión

| Tipo | Sentido | Payload |
|---|---|---|
| `HELLO` | cliente → servidor | identificador de agencia (uint16) |
| `BATCH` | cliente → servidor | cantidad de apuestas (uint32) + las apuestas |
| `ACK` | servidor → cliente | vacío |
| `FINISHED` | cliente → servidor | vacío |
| `WINNERS` | servidor → cliente | cantidad de ganadores (uint32) + las apuestas |
| `ERROR` | servidor → cliente | descripción del fallo en UTF-8 |

La agencia se presenta con un saludo que lleva su identificador; envía sus apuestas en lotes y
espera la confirmación de cada lote antes de armar el siguiente; anuncia con un mensaje de
finalización que no tiene más apuestas; y recibe en un último mensaje las apuestas ganadoras que le
corresponden. Existe además un mensaje de error con el que el servidor informa un fallo en lugar de
cortar la conexión sin explicación.

Que la agencia se identifique **una sola vez**, y no en cada apuesta, responde a que la agencia es
una propiedad de la conexión y no de la apuesta.

Esperar la confirmación de cada lote mantiene **un único mensaje envuelo**, lo que evita que ambos
extremos llenen sus buffers de socket sin drenarlos (un bloqueo mutuo) y le da al servidor un lugar
donde informar el fallo de un lote puntual.

### 3.3. La serialización de una apuesta

La apuesta se serializa campo por campo, en el mismo orden en que aparecen en el registro del
archivo, para que el mapeo entre ambos sea evidente y las dos implementaciones no se desalineen.

| Campo | Representación | Motivo |
|---|---|---|
| nombre, apellido, fecha | un byte de largo + bytes UTF-8 | sin terminador ni relleno |
| documento | uint32 | los valores reales superan los cuarenta millones |
| número | uint32 | los datos de prueba llegan a 99.999, que no entra en 16 bits |

El largo de los campos de texto se cuenta **en bytes y no en caracteres**, porque los nombres llevan
acentos que en UTF-8 ocupan más de un byte y contar caracteres descuadraría al receptor. La fecha de
nacimiento se mantiene como texto porque es un dato opaco para el sistema, que nunca la interpreta;
empaquetarla como fecha sería redundante.

Un campo de texto que superara los 255 bytes no podría representarse, así que se valida y se
devuelve un error de protocolo en lugar de truncarlo en silencio.

El armado y desarmado del paquete está escrito en ambos lados. La conversión entre
enteros y bytes se hace con los ayudantes de orden de bytes de la biblioteca estándar en Go
(`binary.BigEndian.PutUint32` y equivalentes) y con los métodos incorporados de los enteros en
Python (`int.to_bytes` / `int.from_bytes`).

### 3.4. El lote

Cada mensaje de lote lleva la cantidad de apuestas que contiene, seguida de las apuestas
serializadas una tras otra. El cliente arma el lote **incrementalmente en un único buffer**: a
medida que lee cada registro del archivo lo serializa dentro de ese buffer, y cuando el lote se
cierra escribe todo con **una sola llamada al socket**. Esto es esencial, ya que emitir una escritura por apuesta produciría paquetes diminutos y anularía el beneficio del agrupamiento.

Un lote se cierra cuando se alcanza la cantidad configurada en `BATCH_SIZE` o cuando la apuesta
siguiente no entraría en el límite de payload del protocolo, lo que ocurra primero. El límite de
tamaño es la condición dominante: `BATCH_SIZE` es una preferencia y el límite del payload es una
restricción del formato. Al agotarse el archivo se envía el lote parcial si quedó algo pendiente.

Se serializa cada apuesta **dentro de la misma iteración en que se la leyó** ya que los campos de texto de la apuesta son vistas al buffer interno del lector del archivo, válidas solo hasta la lectura siguiente. Retenerlas entre iteraciones para serializarlas al final sería corrupción silenciosa de datos.

### 3.5. Lectura y escritura seguras sobre el descriptor

El contrato de `Read`/`recv` y `Write`/`send` es "hasta N bytes", no "N bytes": `recv` devuelve lo
que ya llegó al buffer de recepción del kernel, sin esperar a completar lo pedido, y `send` copia al
buffer de envío lo que entre y devuelve cuánto copió. Ninguna de las dos situaciones es una
condición de error: son el funcionamiento normal, y ocurren cuando un mensaje viaja partido en
varios segmentos TCP o cuando el buffer de envío está casi lleno. El peligro no es que fallen, sino
que devuelvan éxito habiendo transferido de menos.

Los módulos `safe_socket` de ambos lenguajes resuelven esto con bucles que acumulan hasta cubrir la
cantidad exacta, manteniendo un offset y operando en cada vuelta únicamente sobre lo que falta. En
Go la lectura se hace directamente sobre el slice destino, lo que evita un buffer intermedio; en
Python los pedazos se acumulan en un `bytearray`, cuyo crecimiento está amortizado, y el envío
recorre el buffer con un `memoryview` para no copiar el resto en cada iteración.

Dos detalles condicionan la implementación. Primero, **una escritura de cero bytes no es un error**:
tratarla como tal abortaría envíos válidos, así que el loop simplemente reintenta. Segundo, en la
lectura hay que **contabilizar los bytes leídos antes de evaluar el error**, porque el contrato de
`io.Reader` permite devolver datos y un error en la misma llamada, hacerlo al revés descartaría esos bytes.

El manejo de errores distingue el fin de comunicación legítimo del mensaje truncado. En Go, si el
peer cierra habiendo enviado menos de lo esperado, se devuelve `io.ErrUnexpectedEOF` en lugar del
`io.EOF` crudo. En Python se lanza una excepción propia, `ConnectionClosedError`, que informa cuántos bytes se alcanzaron a recibir de los esperados.

El mismo criterio se aplica a los archivos, el escritor de la salida verifica la cantidad de bytes
efectivamente escritos y no solamente el error, dado que una escritura parcial dejaría el archivo
truncado en silencio.

### 3.6. Validación y cotas

Del lado receptor **no se reserva memoria en función de un largo que declaró el otro extremo sin contrastarlo**. El largo del payload se compara contra una cota fija, y la cantidad de apuestas o de ganadores que anuncia un mensaje se compara contra el tamaño
real del mensaje ya recibido, de modo que un peer defectuoso o malicioso no pueda inducir una
reserva desproporcionada. Al terminar de decodificar se verifica además que no queden bytes
sobrantes en el payload.

---

## 4. Concurrencia y mecanismos de sincronización

### 4.1. Modelo de concurrencia

El servidor atiende **un hilo por conexión**; el hilo principal solo acepta conexiones y lanza. Se
eligió *multithreading* porque la carga es de entrada/salida, los hilos viven bloqueados en el `recv` del socket y en las
operaciones sobre el archivo de apuestas.
El trabajo de CPU puro (decodificar un lote) es redundante frente a esa espera.

Hay **dos estados compartidos distintos, con una primitiva cada uno**. Nunca se toman anidados, se
espera el quórum, se sale de ese bloque, y recién después se toma el lock del archivo, por lo
que no hay un orden de adquisición que respetar ni riesgo de interlocking por construcción.

### 4.2. Primera sección crítica: el almacenamiento de apuestas

`store_bets` y `load_bets` no son seguras para uso concurrente, ya que dos escrituras simultáneas
intercalan filas a medio escribir, y una lectura simultánea con una escritura puede leer una fila
truncada. La solución es garantizar mediante sincronización que un solo hilo acceda a la sección crítica a la vez: hay un
único archivo, un único objeto `Lottery` y un `threading.Lock` que protege ambas operaciones.

Se eligió un **wrapper** (`BetStore`) y no un lock suelto en el servidor para que no exista
ningún camino que llegue al almacenamiento sin pasar por el lock.

- **La comprensión de lista se evalúa entera dentro del lock.** `load_bets` es un generador y,
  devuelto sin consumir, recorrería el archivo de forma lazy ya fuera de la sección crítica.
- **El lock cubre el cálculo, no el envío.** Los ganadores se calculan con el lock tomado, se
  lo libera, y recién después se serializa y se manda. Enviar por el socket con el lock tomado
  dejaría a todo el servidor a la velocidad del cliente más lento.
- La decodificación del lote y la confirmación quedan fuera del lock.

### 4.3. Segunda sección crítica: el quórum de agencias

El sorteo no puede realizarse hasta que un mínimo de agencias haya terminado de cargar. Ese estado
compartido es un **conjunto de identificadores de agencia** porque lo que se
cuenta son agencias distintas, una agencia que se reconectara no debe contar dos veces. Se protege
con una `threading.Condition`, que es un lock más una cola de espera.

La espera se hace con `wait_for(predicado)` y no con un `wait()` a secas. `wait_for` reevalúa el
predicado en un loop, lo cual cubre dos casos que un `wait()` suelto manejaría mal: los spurious wakeups, y el hilo que llega cuando el quórum ya fue alcanzado y por lo tanto no va a recibir ninguna notificación futura.

El registro de la agencia y la espera están separados en dos operaciones para poder registrar la
espera en el log. Es seguro porque el predicado es **monótono**: alcanzado el quórum no vuelve a
perderse, y si se alcanzara justo entre ambas llamadas, `wait_for` lo detecta al evaluar el
predicado con el lock tomado y devuelve sin bloquear. No hay ninguna notificación que perderse.


### 4.4. Cálculo de ganadores

Cada hilo calcula **sus propios** ganadores una vez alcanzado el quórum, con el lock tomado,
filtrando por su agencia. El filtrado vive solo ahí, cada agencia recibe sus ganadores y
nada más, sin difusión de los ganadores de todas.

Se descartó que un único hilo armara un diccionario de agencia a ganadores, porque una agencia que
termina **después** del quórum no estaría en ese diccionario y habría que recalcular igual, con lo
cual no se elimina el caso general. Además, cada agencia ve el archivo con todas sus propias
apuestas ya guardadas, porque las guardó antes de anunciar que terminó, el resultado es correcto sin
importar en qué momento se calcule.

### 4.5. Ciclo de vida de los hilos

El servidor lleva un **registro de conexiones vivas** (el socket y el hilo que lo atiende) protegido
por su propio lock. Cada hilo se desregistra y cierra su socket al terminar, bajo ese lock, de
modo que el registro contiene en todo momento exactamente las conexiones activas y no crece sin
límite. Al cerrar, el servidor espera con `join` a todos los hilos pendientes.

---

## 5. Cierre graceful ante SIGTERM


### 5.1. Patrón aplicado

1. **Un Flag monótono** de cierre: un `threading.Event` en el servidor, un canal cerrado en el
   cliente.
2. **Un mecanismo que desbloquea la llamada bloqueante**, distinto para cada punto de bloqueo. El
   flag por sí sola nunca desbloquea nada.
3. **Reclasificación del error**: la llamada desbloqueada devuelve un error; si el flag está
   encendido, ese error no es una falla sino el cierre, y el proceso termina con código cero.

El handler de la señal hace lo mínimo indispensable (encender la bandera y despertar al hilo
principal) y **todo el trabajo de cierre ocurre en el flujo de control normal**.

### 5.2. Servidor

**Para cortar el `accept`** se espera sobre dos descriptors a la vez: el socket de escucha y la
punta de lectura de un *pipe* interno que el handler escribe. Esto vuelve el cierre inmune a
condiciones de carrera, porque el byte queda escrito, tanto una señal que llegue justo antes de
entrar a la espera como una que la interrumpa terminan despertándola igual. Ningún socket de cliente
participa de esa espera, cada cliente lo sigue atendiendo su propio hilo con `recv` bloqueante, y el
mecanismo no cumple ningún rol en el modelo de concurrencia.

**Para despertar a los hilos que esperan el quórum** se abandona la espera, se enciende un flag
con el lock de la condición tomado y se notifica a todos. El predicado pasa a ser "quórum
alcanzado o cierre en curso", y el hilo que despierta por cierre levanta una excepción propia. El
abandono es monótono igual que el quórum, así que un hilo que llegue después tampoco se cuelga.

**Para despertar a los hilos bloqueados en `recv`** el hilo principal recorre el registro de
conexiones y corta cada una con `shutdown`. Se usa `shutdown` y **no `close`** porque cerrar un
descriptor que otro hilo está usando lo libera, y el número puede ser reasignado a otro recurso, con
lo cual el hilo bloqueado terminaría leyendo de otro lado. `shutdown` no libera el descriptor, solo
corta la conexión, el `recv` bloqueado devuelve vacío y el hilo desenrolla hasta cerrar su socket él
mismo.

La carrera entre ese corte y el cierre que hace cada hilo se elimina por construcción, el hilo
se desregistra y cierra bajo el mismo lock que el hilo principal toma para recorrer el registro,
de modo que todo socket que el hilo principal encuentra está garantizado abierto. El `join`, en
cambio, se hace fuera del lock, porque cada hilo necesita tomarlo para desregistrarse.

La secuencia de cierre es, salir del loop de aceptación, cerrar el socket de escucha, abandonar el
quórum, cortar las conexiones vivas, esperar a los hilos, cerrar el *pipe* y eliminar el archivo
temporal de apuestas. Primero se deja de aceptar y recién después se drenan
los hilos, para que no ingrese una conexión nueva justo después de la espera.

### 5.3. Cliente

El cliente instala el handler y traduce la señal al cierre de un canal, que es la forma
de un flag monótono en Go: todos los interesados se enteran a la vez, se puede consultar sin
bloquear y no hace falta un mutex.

Una goroutine testigo espera ese canal y, al cerrarse, **fija en la conexión un plazo ya vencido**.
Eso hace que cualquier lectura o escritura bloqueada falle de inmediato en lugar de seguir
esperando, **sin invalidar la conexión**: el descriptor lo sigue cerrando su único dueño. La
goroutine no queda colgada cuando el cliente termina normalmente, porque un segundo canal la
finaliza; el orden inverso de ejecución de los diferidos garantiza que muera antes de que se cierre
el socket.

El mecanismo toca **solamente la conexión**. Si la señal llega mientras el cliente está escribiendo
su archivo de ganadores, esa escritura termina, se aborta la comunicación pendiente, no el trabajo
local en curso.

Finalmente, un valor de error distingue el fallo real del cierre ordenado en un único
punto de traducción, y la función principal lo reconoce para terminar con código cero. En ninguno de
los dos procesos el camino de cierre emite registros de falla.


---

## 6. Manejo de errores y liberación de recursos

**Ningún camino termina el proceso de forma forzada.** Las condiciones excepcionales se propagan
como errores o excepciones del programa hasta la función principal, que es el único lugar donde se
traduce el resultado a un código de salida.

**El fallo de un cliente no afecta a los demás.** En el servidor, un error se registra, se le informa
al cliente si el socket todavía lo permite, y se cierra únicamente esa conexión; solamente los
errores del socket de escucha son fatales.

**Todo recurso adquirido se libera por cualquier camino de salida.** Los sockets de cliente y el de
escucha, los dos extremos del *pipe* interno, el archivo temporal donde se almacenan las apuestas,
los archivos de entrada y salida del cliente, y los hilos mediante `join`. En el cliente, el cierre
del archivo de salida se hace sobre un valor de retorno con nombre, de modo que si falla (dejando
potencialmente el archivo incompleto) ese error se propaga en lugar de perderse y hacer que el
proceso termine con éxito.

**El archivo de entrada nunca se carga entero en memoria.** El cliente lo recorre con un lector
incremental y serializa cada apuesta dentro de la iteración en que la leyó, con lo cual su memoria
dinámica queda acotada por el buffer del lector y no crece con el tamaño del conjunto de datos. Del
lado del servidor se recorre el archivo de apuestas con el iterador que provee la clase dada, sin
materializarlo, y se retienen únicamente las apuestas ganadoras de la agencia que pregunta.

---

## 7. Inciso opcional: generador del archivo de composición

Se resolvió el inciso opcional del Ejercicio 1 con el script `generate-compose.py`, ubicado en la
raíz del repositorio:

```bash
./generate-compose.py <archivo_salida> <cantidad_clientes>
```

**El YAML se arma con plantillas de texto y no con una biblioteca.** Se descartó `pyyaml` por dos
motivos: no está garantizado en el entorno donde se ejecute el script, y su volcado reordena las
claves y normaliza el estilo de las comillas, con lo cual el archivo generado no se parecería al que
ya forma parte del repositorio. Con plantillas, la salida es idéntica.

**El archivo de salida es un argumento obligatorio y no tiene valor por defecto.** Un valor por
defecto que apuntara al archivo de composición del repositorio invitaría a sobrescribirlo por
accidente.

**El mínimo de agencias para el sorteo se deriva de la cantidad de clientes** en lugar de fijarse.
Así el archivo generado es coherente por construcción: un mínimo mayor que la cantidad de agencias
configuradas dejaría al sistema esperando indefinidamente un sorteo que no puede ocurrir.

**La ausencia de archivos de entrada se advierte pero no interrumpe la generación.** Si se piden más
clientes que archivos disponibles en `input/`, el script lo informa por la salida de error y genera
el archivo igual, dado que el enunciado admite que esos archivos varíen.
