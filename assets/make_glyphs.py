# Generates the overlay glyph masks (white on transparent, 96x96) and the app icon.
# Original artwork drawn from simple shapes — no game assets.
from PIL import Image, ImageDraw, ImageFilter
import math, os
S=384; OUT=96
W=(255,255,255,255)

def canvas(): 
    im=Image.new('L',(S,S),0); return im, ImageDraw.Draw(im)
def save(im,name):
    im.resize((OUT,OUT),Image.LANCZOS).save(f'glyphs/{name}.png')

def rot(pts,ang,cx=S/2,cy=S/2):
    a=math.radians(ang); c,s=math.cos(a),math.sin(a)
    return [((x-cx)*c-(y-cy)*s+cx,(x-cx)*s+(y-cy)*c+cy) for x,y in pts]

def sword(d,ang,scale=1.0,cx=S/2,cy=S/2):
    k=scale
    blade=[(cx-14*k,cy+40*k),(cx-14*k,cy-120*k),(cx,cy-160*k),(cx+14*k,cy-120*k),(cx+14*k,cy+40*k)]
    guard=[(cx-56*k,cy+40*k),(cx+56*k,cy+40*k),(cx+56*k,cy+62*k),(cx-56*k,cy+62*k)]
    grip=[(cx-10*k,cy+62*k),(cx+10*k,cy+62*k),(cx+10*k,cy+120*k),(cx-10*k,cy+120*k)]
    for p in (blade,guard,grip): d.polygon(rot(p,ang,cx,cy),fill=255)
    px,py=rot([(cx,cy+136*k)],ang,cx,cy)[0]; r=20*k
    d.ellipse([px-r,py-r,px+r,py+r],fill=255)

def shield(d,k=1.0,hole=True):
    cx,cy=S/2,S/2
    pts=[]
    for t in range(0,181):
        a=math.radians(t)
        pass
    top=cy-140*k; w=120*k
    path=[(cx-w,top),(cx+w,top),(cx+w,cy-10*k)]
    for i in range(0,41):
        t=i/40; x=cx+w*(1-t); y=cy-10*k+t*150*k - (t*t)*0
        path.append((cx+w*(1-t)*(1-t*0.15), cy-10*k + 150*k*math.sin(t*math.pi/2)))
    for i in range(40,-1,-1):
        t=i/40
        path.append((cx-w*(1-t)*(1-t*0.15), cy-10*k + 150*k*math.sin(t*math.pi/2)))
    path.append((cx-w,cy-10*k))
    d.polygon(path,fill=255)
    if hole:
        d.rectangle([cx-12*k,top+36*k,cx+12*k,cy+100*k],fill=0)
        d.rectangle([cx-70*k,cy-40*k,cx+70*k,cy-16*k],fill=0)

# --- classes ---
im,d=canvas(); sword(d,-35,1.15); save(im,'class_11')                    # Gladiador
im,d=canvas(); shield(d,1.05); save(im,'class_12')                        # Templário
im,d=canvas()                                                              # Assassino: 2 crossed daggers
for ang in (-40,40):
    k=1.15; cx,cy=S/2,S/2+10
    blade=[(cx-16*k,cy+20*k),(cx-6*k,cy-150*k),(cx+16*k,cy-110*k),(cx+16*k,cy+20*k)]
    guard=[(cx-44*k,cy+20*k),(cx+44*k,cy+20*k),(cx+44*k,cy+40*k),(cx-44*k,cy+40*k)]
    grip=[(cx-10*k,cy+40*k),(cx+10*k,cy+40*k),(cx+10*k,cy+100*k),(cx-10*k,cy+100*k)]
    for p in (blade,guard,grip): d.polygon(rot(p,ang,S/2,S/2+10),fill=255)
save(im,'class_13')
im,d=canvas()                                                              # Ranger: bow + arrow
d.arc([70,50,250,334],-80,80,fill=255,width=34)
top=(160+ 90*math.cos(math.radians(-80)), 192+142*math.sin(math.radians(-80)))
bot=(160+ 90*math.cos(math.radians(80)), 192+142*math.sin(math.radians(80)))
d.line([top,bot],fill=255,width=8)
d.line([(110,192),(320,192)],fill=255,width=16)
d.polygon([(330,192),(286,166),(286,218)],fill=255)
d.polygon([(110,192),(80,166),(96,192),(80,218)],fill=255)
save(im,'class_14')
im,d=canvas()                                                              # Feiticeiro: flame
big=Image.new('L',(S,S),0); bd=ImageDraw.Draw(big)
bd.ellipse([92,150,292,350],fill=255)
bd.polygon([(98,240),(192,20),(286,240)],fill=255)
bd.polygon([(110,230),(70,110),(170,190)],fill=255)
bd.polygon([(274,230),(320,130),(220,190)],fill=255)
big=big.filter(ImageFilter.GaussianBlur(14)).point(lambda v:255 if v>128 else 0).filter(ImageFilter.GaussianBlur(1.5))
cut=Image.new('L',(S,S),0); cd=ImageDraw.Draw(cut)
cd.ellipse([152,232,232,312],fill=255); cd.polygon([(154,268),(192,150),(230,268)],fill=255)
cut=cut.filter(ImageFilter.GaussianBlur(8)).point(lambda v:255 if v>128 else 0)
im=Image.composite(Image.new('L',(S,S),0),big,cut)
save(im,'class_15')
im,d=canvas()                                                              # Elementalista: orb + orbit
d.ellipse([132,132,252,252],fill=255)
d.ellipse([52,112,332,272],outline=255,width=20)
d.ellipse([290,96,346,152],fill=255)
d.ellipse([46,236,90,280],fill=255)
save(im,'class_16')
im2=im.copy(); save(im2,'class_10')
im,d=canvas()                                                              # Clérigo: cross + halo
d.rounded_rectangle([162,70,222,330],radius=24,fill=255)
d.rounded_rectangle([86,140,298,200],radius=24,fill=255)
d.arc([110,20,274,110],200,340,fill=255,width=16)
save(im,'class_17')
im,d=canvas()                                                              # Chanter: staff with ring
d.line([(192,120),(192,350)],fill=255,width=26)
d.ellipse([132,30,252,150],outline=255,width=22)
d.ellipse([174,72,210,108],fill=255)
d.polygon([(130,190),(60,150),(90,210),(150,215)],fill=255)
d.polygon([(254,190),(324,150),(294,210),(234,215)],fill=255)
save(im,'class_18')
im,d=canvas()                                                              # Brawler: fist
for i in range(4):
    x=96+i*50; d.rounded_rectangle([x,96,x+44,190],radius=20,fill=255)
d.rounded_rectangle([92,160,300,300],radius=40,fill=255)
d.rounded_rectangle([52,170,130,250],radius=30,fill=255)
d.line([(110,215),(200,215)],fill=0,width=12)
save(im,'class_19')

# --- tabs ---
im,d=canvas(); sword(d,-45,0.95,S/2,S/2); sword(d,45,0.95,S/2,S/2); save(im,'tab_dps')
im,d=canvas()                                                              # heal: heart with plus
d.ellipse([60,70,200,210],fill=255); d.ellipse([184,70,324,210],fill=255)
d.polygon([(66,170),(318,170),(192,330)],fill=255)
d.rectangle([174,110,210,250],fill=0); d.rectangle([122,162,262,198],fill=0)
save(im,'tab_heal')
im,d=canvas(); shield(d,1.05,hole=False)
d.polygon([(192,100),(192,330),(110,250),(100,90)],fill=0)  # half-shade
im2=Image.new('L',(S,S),0); dd=ImageDraw.Draw(im2); shield(dd,1.05,hole=False)
im=Image.composite(Image.new('L',(S,S),255),im2,im2)
d=ImageDraw.Draw(im); 
inner=Image.new('L',(S,S),0); di=ImageDraw.Draw(inner); shield(di,0.72,hole=False)
im=Image.eval(Image.composite(Image.new('L',(S,S),0),im,inner),lambda v:v)
d=ImageDraw.Draw(im); d.rectangle([180,90,204,330],fill=255)
im=Image.composite(im,Image.new('L',(S,S),0),im2)
save(im,'tab_tank')

# --- app icon ---
B=1024
icon=Image.new('RGBA',(B,B),(0,0,0,0))
bg=Image.new('RGBA',(B,B),(0,0,0,0))
grad=Image.new('RGBA',(B,B))
for y in range(B):
    for x in range(0,B,8):
        pass
import numpy as np
yy,xx=np.mgrid[0:B,0:B]
dist=np.sqrt((xx-B*0.5)**2+(yy-B*0.38)**2)/(B*0.75)
c1=np.array([58,40,120]); c2=np.array([10,12,24])
t=np.clip(dist,0,1)[...,None]
rgb=(c1*(1-t)+c2*t).astype(np.uint8)
grad=Image.fromarray(np.dstack([rgb,np.full((B,B),255,np.uint8)]),'RGBA')
mask=Image.new('L',(B,B),0); ImageDraw.Draw(mask).rounded_rectangle([24,24,B-24,B-24],radius=220,fill=255)
icon.paste(grad,(0,0),mask)
# aether wings: curved feathers fanning up behind the bars
wing=Image.new('L',(B,B),0)
def feather(L,Wd,ang,px,py):
    f=Image.new('L',(B,B),0); fd=ImageDraw.Draw(f)
    fd.ellipse([px-L,py-Wd/2,px,py+Wd/2],fill=255)        # points left from pivot
    return f.rotate(ang,center=(px,py),resample=Image.BICUBIC)
px,py=B/2-70,B*0.76
for i in range(7):
    L=230+i*32; Wd=58+i*2; ang=-(12+i*10)
    wing=Image.composite(Image.new('L',(B,B),255),wing,feather(L,Wd,ang,px,py))
wing=wing.filter(ImageFilter.GaussianBlur(1.2))
wingR=wing.transpose(Image.FLIP_LEFT_RIGHT)
wings=Image.composite(Image.new('L',(B,B),255),wing,wingR)
glow=wings.filter(ImageFilter.GaussianBlur(30))
gl=Image.new('RGBA',(B,B),(80,200,255,0)); gl.putalpha(glow.point(lambda v:int(v*0.65)))
icon=Image.alpha_composite(icon,gl)
wc=Image.new('RGBA',(B,B),(175,232,255,255)); wc.putalpha(wings.point(lambda v:int(v*0.95)))
icon=Image.alpha_composite(icon,wc)
# feather separation lines
sep=Image.new('RGBA',(B,B),(0,0,0,0))
# center bars (gold) like a meter
bars=Image.new('RGBA',(B,B),(0,0,0,0)); bd=ImageDraw.Draw(bars)
bw=70; gap=26; x0=B/2-(3*bw+2*gap)/2; base=B*0.80
for i,h in enumerate((0.20,0.32,0.46)):
    x=x0+i*(bw+gap); top=base-h*B
    bd.rounded_rectangle([x,top,x+bw,base],radius=22,fill=(255,196,70,255))
bglow=bars.split()[3].filter(ImageFilter.GaussianBlur(24))
g2=Image.new('RGBA',(B,B),(255,170,40,0)); g2.putalpha(bglow.point(lambda v:int(v*0.8)))
icon=Image.alpha_composite(icon,g2); icon=Image.alpha_composite(icon,bars)
# border
bd2=Image.new('RGBA',(B,B),(0,0,0,0)); ImageDraw.Draw(bd2).rounded_rectangle([24,24,B-24,B-24],radius=220,outline=(120,200,255,200),width=14)
icon=Image.alpha_composite(icon,bd2)
icon.save('appicon.png')
icon.save('appicon.ico',sizes=[(16,16),(24,24),(32,32),(48,48),(64,64),(128,128),(256,256)])
print('ok')
